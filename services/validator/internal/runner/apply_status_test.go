package runner

// Regression cover for the "green suite over an unpatched tree" bug.
//
// The sandbox used to run `git apply patch.diff || true`, so a patch the tree
// rejected was skipped and pytest went on to pass against the ORIGINAL code.
// The caller was told tests_passed=true, and the approval gate presented that
// as a validated patch. These cases pin the decision table that replaced it.

import "testing"

func TestApplyStatus(t *testing.T) {
	cases := []struct {
		name        string
		stdout      string
		hadPatch    bool
		wantApplied bool
		wantErr     bool
	}{
		{
			name:        "patch applied",
			stdout:      "Checking patch src/x.py...\n" + patchAppliedMarker + "\n12 passed",
			hadPatch:    true,
			wantApplied: true,
		},
		{
			name:        "patch rejected",
			stdout:      "error: patch failed: src/x.py:14\n" + patchFailedMarker + "\n12 passed",
			hadPatch:    true,
			wantApplied: false,
			wantErr:     true,
		},
		{
			name:        "no patch sent, sandbox saw none",
			stdout:      patchEmptyMarker + "\n12 passed",
			hadPatch:    false,
			wantApplied: true,
		},
		{
			name:        "patch sent but sandbox saw an empty file",
			stdout:      patchEmptyMarker + "\n12 passed",
			hadPatch:    true,
			wantApplied: false,
			wantErr:     true,
		},
		{
			name:        "no marker at all with a patch",
			stdout:      "sh: something exploded",
			hadPatch:    true,
			wantApplied: false,
			wantErr:     true,
		},
		{
			name:        "no marker at all without a patch",
			stdout:      "sh: something exploded",
			hadPatch:    false,
			wantApplied: true,
		},
		{
			// A rejection anywhere in the stream wins: git prints its own
			// noise before the marker and pytest prints plenty after it.
			name:        "failure marker wins over applied text elsewhere",
			stdout:      "applied cleanly? no\n" + patchFailedMarker,
			hadPatch:    true,
			wantApplied: false,
			wantErr:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied, errMsg := applyStatus(tc.stdout, tc.hadPatch)
			if applied != tc.wantApplied {
				t.Errorf("applied = %v, want %v", applied, tc.wantApplied)
			}
			if (errMsg != "") != tc.wantErr {
				t.Errorf("errMsg = %q, want error: %v", errMsg, tc.wantErr)
			}
		})
	}
}
