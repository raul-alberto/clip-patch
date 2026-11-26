// Package apply turns a parsed patch into a validated plan and executes it
// with backups, staging and rollback.
package apply

import (
	"bytes"
	"fmt"
	"os"

	"clip-patch/internal/patch"
	"clip-patch/internal/safefs"
)

// Change is the computed result for one target file.
type Change struct {
	File     *safefs.File
	NewText  string
	NewBytes []byte
	Warnings []string
	Edits    int // number of SEARCH/REPLACE blocks or hunks
}

// Changed reports whether the patch alters the file's content.
func (c *Change) Changed() bool { return !bytes.Equal(c.File.Raw, c.NewBytes) }

// Plan is a fully validated patch, computed entirely in memory.
type Plan struct {
	Format  patch.Format
	RootAbs string
	Changes []*Change
}

// ChangedFiles returns only the changes that alter file content.
func (p *Plan) ChangedFiles() []*Change {
	var out []*Change
	for _, c := range p.Changes {
		if c.Changed() {
			out = append(out, c)
		}
	}
	return out
}

// BuildPlan validates every path and every modification and computes all new
// file contents in memory. It never writes anything. If any part of the patch
// is invalid, an error is returned and nothing has been modified.
func BuildPlan(root string, p *patch.Patch) (*Plan, error) {
	rootAbs, rootReal, err := safefs.ResolveRoot(root)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Format: p.Format, RootAbs: rootAbs}
	seen := map[string]string{}

	for _, fp := range p.Files {
		rel, err := safefs.ValidateRelPath(fp.Path)
		if err != nil {
			return nil, err
		}
		if prev, dup := seen[rel]; dup {
			return nil, &patch.Error{
				Title:  "Conflicting modifications.",
				File:   rel,
				Reason: fmt.Sprintf("the file is targeted more than once (as %q and %q).", prev, fp.Path),
			}
		}
		seen[rel] = fp.Path

		f, err := safefs.Load(rootAbs, rootReal, rel)
		if err != nil {
			return nil, err
		}

		var newText string
		var warnings []string
		edits := 0
		switch p.Format {
		case patch.FormatSearchReplace:
			newText, err = patch.ApplyEdits(rel, f.Text, fp.Edits)
			edits = len(fp.Edits)
		case patch.FormatUnifiedDiff:
			newText, warnings, err = patch.ApplyHunks(rel, f.Text, fp.Hunks)
			edits = len(fp.Hunks)
		default:
			err = fmt.Errorf("unsupported patch format %q", p.Format)
		}
		if err != nil {
			return nil, err
		}
		plan.Changes = append(plan.Changes, &Change{
			File:     f,
			NewText:  newText,
			NewBytes: safefs.Render(f, newText),
			Warnings: warnings,
			Edits:    edits,
		})
	}
	return plan, nil
}

// VerifyUnchanged re-reads every target and checks that its content still
// matches the fingerprint taken when the plan was built.
func (p *Plan) VerifyUnchanged() error {
	for _, c := range p.Changes {
		cur, err := os.ReadFile(c.File.Abs)
		if err != nil {
			return &patch.Error{
				Title:  "File changed since the preview.",
				File:   c.File.Rel,
				Reason: "the file can no longer be read: " + err.Error(),
			}
		}
		if safefs.Fingerprint(cur) != c.File.Hash {
			return &patch.Error{
				Title:  "File changed since the preview.",
				File:   c.File.Rel,
				Reason: "its content differs from when the preview was generated. Run clip-patch again to create a fresh preview.",
			}
		}
	}
	return nil
}
