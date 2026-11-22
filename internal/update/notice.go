package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CheckInterval is how often the notice contacts GitHub.
const CheckInterval = 24 * time.Hour

type noticeState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
}

// Notifier tells the user about a newer release, contacting GitHub at most
// once per CheckInterval. Failed checks are recorded too, so an offline
// computer doesn't retry on every command.
type Notifier struct {
	Client    Client
	StatePath string
	Current   string
	Now       func() time.Time
}

// StatePath is where the last check is cached.
func StatePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "store", "update-check.json"), nil
}

// Notice returns a one-line message when a newer release is known, or "".
func (n Notifier) Notice(ctx context.Context) string {
	now := time.Now
	if n.Now != nil {
		now = n.Now
	}
	st := n.load()
	if st.CheckedAt.IsZero() || now().Sub(st.CheckedAt) >= CheckInterval || now().Before(st.CheckedAt) {
		st.CheckedAt = now()
		if rel, err := n.Client.Latest(ctx); err == nil {
			st.Latest = rel.Version
		}
		n.save(st)
	}
	if !Newer(st.Latest, n.Current) {
		return ""
	}
	return fmt.Sprintf("A new version of store is available: %s (you have %s). Run `store update` to install it.", st.Latest, n.Current)
}

func (n Notifier) load() noticeState {
	var st noticeState
	data, err := os.ReadFile(n.StatePath)
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func (n Notifier) save(st noticeState) {
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(n.StatePath), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(n.StatePath), ".update-check-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr != nil || cerr != nil {
		return
	}
	_ = os.Rename(tmp.Name(), n.StatePath)
}
