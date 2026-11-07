package domain

// Progress summarizes the todos of a project (or of the inbox, or of all the
// projects in a collection). The zero value is an empty summary.
type Progress struct {
	Total    int
	ByStatus map[Status]int
	Overdue  int
}

// AddCount records count todos in status, overdue of which are overdue.
func (p *Progress) AddCount(status Status, count, overdue int) {
	if p.ByStatus == nil {
		p.ByStatus = map[Status]int{}
	}
	p.Total += count
	p.ByStatus[status] += count
	p.Overdue += overdue
}

// Add merges another summary into p (collections roll up their projects).
func (p *Progress) Add(o Progress) {
	for status, n := range o.ByStatus {
		p.AddCount(status, n, 0)
	}
	p.Overdue += o.Overdue
}

// PercentDone is done / (total - cancelled), rounded down; cancelled todos
// don't count as remaining work. A project with nothing to do is 0%.
func (p Progress) PercentDone() int {
	relevant := p.Total - p.ByStatus[StatusCancelled]
	if relevant <= 0 {
		return 0
	}
	return p.ByStatus[StatusDone] * 100 / relevant
}
