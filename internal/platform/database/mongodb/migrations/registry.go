package migrations

import (
	"fmt"
	"sort"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

type MigrationFunc func(db *mongo.Database) error
type RollbackFunc func(db *mongo.Database) error

type MigrationEntry struct {
	Version  int
	Name     string
	Migrate  MigrationFunc
	Rollback RollbackFunc
}

var registry = make(map[int]MigrationEntry)

func Register(version int, name string, migrate MigrationFunc, rollback RollbackFunc) {
	if _, exists := registry[version]; exists {
		panic(fmt.Sprintf("Migration version %d already exists", version))
	}
	registry[version] = MigrationEntry{
		Version:  version,
		Name:     name,
		Migrate:  migrate,
		Rollback: rollback,
	}
}

func GetMigrations() []MigrationEntry {
	var sorted []MigrationEntry
	for _, m := range registry {
		sorted = append(sorted, m)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version < sorted[j].Version
	})
	return sorted
}

func GetLatestVersion() int {
	latest := 0
	for version := range registry {
		if version > latest {
			latest = version
		}
	}
	return latest
}
