package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
)

// Fetcher is the gateway's internal curriculum API.
type Fetcher interface {
	Scenario(ctx context.Context, lessonID, etag string) (raw []byte, newETag string, notModified bool, err error)
	Fixtures(ctx context.Context, lessonID, etag string) (gwclient.FixtureResult, error)
}

// FetchScenario fetches a lesson's scenario through the ETag cache under
// cacheDir. When the gateway is unreachable the cached copy is used; without
// one the error is httpx.SessionUnavailable. An unknown lesson is
// httpx.NotFound.
func FetchScenario(ctx context.Context, fetch Fetcher, cacheDir, lessonID string, log *slog.Logger) (*protocol.Scenario, error) {
	p := cachePath(cacheDir, "scenarios", lessonID, ".json")
	raw, newETag, notModified, err := fetch.Scenario(ctx, lessonID, cachedETag(p))
	switch {
	case err != nil && gwclient.NotFound(err):
		return nil, httpx.NotFound("no such lesson: " + lessonID)
	case err != nil:
		cached, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil, httpx.SessionUnavailable("could not fetch the scenario from the gateway").Wrap(err)
		}
		logger(log).Warn("gateway unreachable; using the cached scenario", "lesson", lessonID, "err", err)
		raw = cached
	case notModified:
		cached, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil, httpx.Internal("scenario cache missing").Wrap(rerr)
		}
		raw = cached
	default:
		if newETag != "" {
			if err := writeCached(p, raw, newETag); err != nil {
				logger(log).Warn("caching scenario", "err", err)
			}
		}
	}
	var sc protocol.Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, httpx.Internal("the gateway sent an invalid scenario").Wrap(err)
	}
	if sc.ID == "" {
		sc.ID = lessonID
	}
	sc.AssignIDs()
	return &sc, nil
}

// FetchFixtures fetches the lesson's fixtures through the ETag cache under
// cacheDir and extracts them into root below the setup root (Extract); it
// returns the number of files written. A lesson without fixtures writes
// none; the gateway down with nothing cached is httpx.SessionUnavailable.
func FetchFixtures(ctx context.Context, fetch Fetcher, cacheDir string, root *os.Root, sc *protocol.Scenario, reset bool, log *slog.Logger) (int, error) {
	p := cachePath(cacheDir, "fixtures", sc.ID, protocol.FixtureExt)
	fr, err := fetch.Fixtures(ctx, sc.ID, cachedETag(p))
	switch {
	case err != nil && gwclient.NotFound(err):
		return 0, nil
	case err != nil:
		if _, serr := os.Stat(p); serr != nil {
			return 0, httpx.SessionUnavailable("could not fetch the lesson's files from the gateway").Wrap(err)
		}
		logger(log).Warn("gateway unreachable; using cached fixtures", "lesson", sc.ID, "err", err)
	case fr.None:
		_ = os.Remove(p)
		_ = os.Remove(p + ".etag")
		return 0, nil
	case fr.NotModified:
	default:
		data, rerr := io.ReadAll(io.LimitReader(fr.Body, MaxFixtureTotal))
		fr.Body.Close()
		if rerr != nil {
			return 0, httpx.SessionUnavailable("could not download the lesson's files").Wrap(rerr)
		}
		if err := writeCached(p, data, fr.ETag); err != nil {
			return 0, httpx.Internal("could not cache the lesson's files").Wrap(err)
		}
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	res, err := Extract(f, root, sc.Setup.Root, reset)
	if err != nil {
		return res.Written, httpx.Internal("could not unpack the lesson's files").Wrap(err)
	}
	logger(log).Info("fixtures extracted", "lesson", sc.ID, "root", sc.Setup.Root, "written", res.Written, "skipped", res.Skipped, "removed", res.Removed)
	return res.Written, nil
}

func logger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.Default()
	}
	return l
}

func cachePath(cacheDir, kind, lessonID, ext string) string {
	return filepath.Join(cacheDir, kind, filepath.FromSlash(lessonID)+ext)
}

// cachedETag returns the ETag of the cached file p, "" when p is not cached.
func cachedETag(p string) string {
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	b, err := os.ReadFile(p + ".etag")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeCached(p string, data []byte, etag string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(p+".tmp", p); err != nil {
		return err
	}
	return os.WriteFile(p+".etag", []byte(etag), 0o644)
}
