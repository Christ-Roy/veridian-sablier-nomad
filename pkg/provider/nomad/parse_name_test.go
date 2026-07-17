package nomad

import "testing"

func TestParseName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		opts     ParseOptions
		expected ParsedName
		hasError bool
	}{
		{
			name:     "job id only",
			input:    "whoami",
			opts:     ParseOptions{Delimiter: "@"},
			expected: ParsedName{Original: "whoami", JobID: "whoami"},
		},
		{
			name:     "job and group",
			input:    "whoami@web",
			opts:     ParseOptions{Delimiter: "@"},
			expected: ParsedName{Original: "whoami@web", JobID: "whoami", Group: "web"},
		},
		{
			name:     "job group and replicas",
			input:    "whoami@web@3",
			opts:     ParseOptions{Delimiter: "@"},
			expected: ParsedName{Original: "whoami@web@3", JobID: "whoami", Group: "web", Replicas: 3},
		},
		{
			name:     "hyphenated job id single token",
			input:    "apical-medusa",
			opts:     ParseOptions{Delimiter: "@"},
			expected: ParsedName{Original: "apical-medusa", JobID: "apical-medusa"},
		},
		{
			name:     "custom delimiter",
			input:    "job/group/2",
			opts:     ParseOptions{Delimiter: "/"},
			expected: ParsedName{Original: "job/group/2", JobID: "job", Group: "group", Replicas: 2},
		},
		{
			name:     "empty name",
			input:    "",
			opts:     ParseOptions{Delimiter: "@"},
			hasError: true,
		},
		{
			name:     "too many parts",
			input:    "a@b@c@d",
			opts:     ParseOptions{Delimiter: "@"},
			hasError: true,
		},
		{
			name:     "non numeric replicas",
			input:    "job@group@two",
			opts:     ParseOptions{Delimiter: "@"},
			hasError: true,
		},
		{
			name:     "negative replicas",
			input:    "job@group@-1",
			opts:     ParseOptions{Delimiter: "@"},
			hasError: true,
		},
		{
			name:     "empty job id",
			input:    "@group",
			opts:     ParseOptions{Delimiter: "@"},
			hasError: true,
		},
		{
			name:     "empty group",
			input:    "job@",
			opts:     ParseOptions{Delimiter: "@"},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseName(tt.input, tt.opts)
			if tt.hasError {
				if err == nil {
					t.Fatalf("expected error but got nil (result %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error but got %v", err)
			}
			if got != tt.expected {
				t.Errorf("expected %+v but got %+v", tt.expected, got)
			}
		})
	}
}

func TestJobInstanceName(t *testing.T) {
	opts := ParseOptions{Delimiter: "@"}

	if got := JobInstanceName("whoami", "web", true, opts); got != "whoami" {
		t.Errorf("single-group job should use bare job id, got %q", got)
	}
	if got := JobInstanceName("whoami", "web", false, opts); got != "whoami@web" {
		t.Errorf("multi-group job should be qualified, got %q", got)
	}

	// Round-trips through ParseName for the multi-group form.
	name := JobInstanceName("whoami", "web", false, opts)
	parsed, err := ParseName(name, opts)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if parsed.JobID != "whoami" || parsed.Group != "web" {
		t.Errorf("round-trip mismatch: %+v", parsed)
	}
}
