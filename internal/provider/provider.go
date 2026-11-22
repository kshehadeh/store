// Package provider defines the interface between store's core and a secure
// third-party vault such as 1Password.
package provider

import (
	"context"
	"io"
	"time"
)

// Kind describes how an item's content can be retrieved.
type Kind string

const (
	// KindDocument items hold a single file.
	KindDocument Kind = "document"
	// KindNote items hold free-form note text.
	KindNote Kind = "note"
	// KindFields items hold structured fields (logins, API credentials, ...).
	KindFields Kind = "fields"
)

// Item is vault metadata. It never contains secret values.
type Item struct {
	ID       string
	Title    string
	Category string // provider category for display, e.g. "Login"
	Kind     Kind
	Tags     []string
	Updated  time.Time
}

// Field describes a field on a structured item, without its value.
type Field struct {
	ID      string
	Label   string
	Section string
	Type    string
}

// Detail is an item plus the metadata needed to retrieve its content.
type Detail struct {
	Item
	FileName string
	Fields   []Field
}

// Document is content to create or replace as a native document item.
type Document struct {
	Title       string
	FileName    string
	Tags        []string
	Description *string
	Content     io.Reader
}

// Check is one line of a status report.
type Check struct {
	Name   string
	OK     bool
	Detail string
	Fix    string
}

// Provider is a configured connection to one vault.
type Provider interface {
	// Status checks connectivity without writing to the vault.
	Status(ctx context.Context) []Check
	// ListItems returns metadata for every item in the vault.
	ListItems(ctx context.Context) ([]Item, error)
	GetItem(ctx context.Context, id string) (Detail, error)
	CreateDocument(ctx context.Context, doc Document) (string, error)
	// UpdateDocument replaces the content, file name, title, and tags of a
	// document item in a single operation.
	UpdateDocument(ctx context.Context, id string, doc Document) error
	// RenameItem changes an item's title without changing its other fields.
	RenameItem(ctx context.Context, id, title string) error
	// DownloadDocument writes a document's file to path with private
	// permissions. path must not exist.
	DownloadDocument(ctx context.Context, id, path string) error
	ReadNote(ctx context.Context, id string) (string, error)
	ReadField(ctx context.Context, id, fieldID string) (string, error)
}

// UI is the interaction a provider may need during setup.
type UI interface {
	Choose(prompt string, options []string) (int, error)
	Info(format string, args ...any)
}

// Definition describes a provider that store can be set up with.
type Definition struct {
	Name        string
	DisplayName string
	// Setup chooses the provider-specific settings interactively, using any
	// preset values supplied on the command line, and verifies access.
	// Settings hold identifiers only, never credentials.
	Setup func(ctx context.Context, ui UI, preset map[string]string) (map[string]string, error)
	Open  func(settings map[string]string) (Provider, error)
	// Describe returns a human-readable location, e.g. `1Password vault "Shared"`.
	Describe func(settings map[string]string) string
}
