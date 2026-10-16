package shared

import "context"

type Job struct {
	Source      string
	ExternalID  string
	Platform    string
	Company     string
	Title       string
	Location    string
	URL         string
	Description string
	Skills      []string
	PostedAt    string
}

type WorkMode string

const (
	Remote     WorkMode = "remote"
	Relocation WorkMode = "relocation"
)

type Source interface {
	Name() string
	Search(ctx context.Context, intent Intent) ([]Job, error)
}
