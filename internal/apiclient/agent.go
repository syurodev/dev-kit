package apiclient

import (
	"fmt"
	"sort"
	"strings"
)

// What an agent may write, and what the person is asked about first, is the
// MCP layer's decision (internal/mcp/tools_apiclient.go). The two answers it
// needs about a request are here, next to the rules they restate: which
// credentials a saved copy would hand to the host it names, and what a masked
// row an agent read back stands for.

// Carries names what a saved copy of req would hand to the host it points at:
// its bound account, every secret variable it fills in under ANY environment
// (the one it is sent against is chosen later, by whoever sends it), and a
// value typed into a row named like a credential.
//
// A saved request is the person's: api.send fires it on the environment's
// rule alone, without the Adhoc check Send makes of an agent's own request.
// So an agent saving one that carries any of these is choosing where a
// credential goes, and that is the question the person must answer first.
func (s *Service) Carries(req Request) ([]string, error) {
	req = req.normalized()
	var out []string
	if req.Auth.Kind != AuthNone && req.Auth.AccountID != "" {
		out = append(out, "the account "+req.Auth.AccountID)
	}

	secret := map[string]bool{}
	scopes := []string{""}
	environments, err := s.Environments()
	if err != nil {
		return nil, err
	}
	for _, env := range environments {
		scopes = append(scopes, env.ID)
	}
	for _, envID := range scopes {
		scope, err := s.resolve(req.CollectionID, envID)
		if err != nil {
			return nil, err
		}
		for _, name := range secretsUsed(req, scope) {
			secret[name] = true
		}
	}
	names := make([]string, 0, len(secret))
	for name := range secret {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, "the secret {{"+name+"}}")
	}

	for _, table := range []struct {
		label string
		rows  KVList
	}{{"header", req.Headers}, {"query parameter", req.Query}, {"form field", req.Form}} {
		for _, row := range table.rows.Active() {
			if sensitiveName(row.Key) && row.Value != "" && !onlyVariables(row.Value) {
				out = append(out, "a value in the "+table.label+" "+row.Key)
			}
		}
	}
	return out, nil
}

// RestoreMasked puts back the values an agent was shown masked.
//
// api.getRequest masks every credential-named row (ForAgent). An agent that
// edits the request and saves it hands those rows back as Masked; stored as
// is, the token somebody typed would be replaced by bullets. A masked row
// therefore keeps the stored row of the same name — the agent cannot know the
// value, so it cannot have meant to change it. A masked value with nothing to
// keep (a new request, a renamed row) is refused rather than saved.
func (s *Service) RestoreMasked(req Request) (Request, error) {
	var stored Request
	if req.ID != "" {
		var err error
		if stored, err = s.Request(req.ID); err != nil {
			return Request{}, ErrNotFound
		}
	}
	var err error
	if req.Headers, err = restoreRows(req.Headers, stored.Headers, true, "header"); err != nil {
		return Request{}, err
	}
	if req.Query, err = restoreRows(req.Query, stored.Query, false, "query parameter"); err != nil {
		return Request{}, err
	}
	if req.Form, err = restoreRows(req.Form, stored.Form, false, "form field"); err != nil {
		return Request{}, err
	}
	return req, nil
}

func restoreRows(rows, stored KVList, foldCase bool, label string) (KVList, error) {
	if rows == nil {
		return nil, nil
	}
	out := make(KVList, len(rows))
	for i, row := range rows {
		out[i] = row
		if row.Value != Masked {
			continue
		}
		found := false
		for _, original := range stored {
			if original.Key == row.Key || (foldCase && strings.EqualFold(original.Key, row.Key)) {
				out[i].Value, found = original.Value, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: the %s %s is masked and has no stored value to keep — use a {{variable}} or an account instead", ErrInvalid, label, row.Key)
		}
	}
	return out, nil
}
