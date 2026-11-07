// Package firebase connects the identity module to Firebase Authentication
// with the Firebase Admin SDK: it verifies ID tokens (IDTokenVerifier) and,
// with a service account, mints custom tokens and deletes users
// (FirebaseAdmin).
//
// Verifying ID tokens needs only the project ID: the SDK checks them against
// Google's public keys, which it caches. Setting FIREBASE_AUTH_EMULATOR_HOST
// points the SDK at the Auth emulator, which accepts unsigned tokens.
package firebase

import (
	"context"
	"errors"
	"fmt"
	"time"

	fb "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
)

type Client struct{ auth *auth.Client }

var (
	_ application.IDTokenVerifier = (*Client)(nil)
	_ application.FirebaseAdmin   = (*Client)(nil)
)

// New connects to projectID. credentialsFile (a service account JSON key)
// is optional; without it the client only verifies ID tokens, and
// Admin reports that it can't do more.
func New(ctx context.Context, projectID, credentialsFile string) (*Client, error) {
	if projectID == "" {
		return nil, errors.New("firebase: project ID is required")
	}
	opt := option.WithoutAuthentication()
	if credentialsFile != "" {
		opt = option.WithCredentialsFile(credentialsFile)
	}
	app, err := fb.NewApp(ctx, &fb.Config{ProjectID: projectID}, opt)
	if err != nil {
		return nil, fmt.Errorf("firebase: %w", err)
	}
	c, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth: %w", err)
	}
	return &Client{auth: c}, nil
}

func (c *Client) VerifyIDToken(ctx context.Context, idToken string) (application.IDClaims, error) {
	if idToken == "" {
		return application.IDClaims{}, domain.ErrInvalidIDToken
	}
	tok, err := c.auth.VerifyIDToken(ctx, idToken)
	switch {
	case err == nil:
	case auth.IsIDTokenExpired(err):
		return application.IDClaims{}, domain.ErrExpiredIDToken
	case auth.IsCertificateFetchFailed(err):
		return application.IDClaims{}, fmt.Errorf("firebase: fetch signing keys: %w", err)
	default:
		return application.IDClaims{}, domain.ErrInvalidIDToken
	}
	return claimsOf(tok), nil
}

func claimsOf(tok *auth.Token) application.IDClaims {
	str := func(k string) string { s, _ := tok.Claims[k].(string); return s }
	verified, _ := tok.Claims["email_verified"].(bool)
	return application.IDClaims{
		UID:           tok.UID,
		Email:         str("email"),
		EmailVerified: verified,
		Name:          str("name"),
		Provider:      tok.Firebase.SignInProvider,
		AuthTime:      time.Unix(tok.AuthTime, 0).UTC(),
	}
}

func (c *Client) CustomToken(ctx context.Context, firebaseUID string) (string, error) {
	return c.auth.CustomToken(ctx, firebaseUID)
}

func (c *Client) DeleteUser(ctx context.Context, firebaseUID string) error {
	err := c.auth.DeleteUser(ctx, firebaseUID)
	if auth.IsUserNotFound(err) {
		return nil // already gone
	}
	return err
}
