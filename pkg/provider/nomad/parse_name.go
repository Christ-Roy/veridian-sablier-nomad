package nomad

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsedName is the decomposed form of a Sablier instance name targeting a
// Nomad task group.
type ParsedName struct {
	Original string
	JobID    string
	// Group is the task group name. An empty Group means "resolve the single
	// task group of the job at runtime".
	Group string
	// Replicas is the desired replica count encoded in the name. Zero means
	// unset: the sablier.active.replicas label (default 1) is used instead.
	Replicas int
}

// ParseOptions controls how instance names are split.
type ParseOptions struct {
	Delimiter string
}

// ParseName decomposes a Sablier instance name into its Nomad coordinates.
// Accepted forms (with the default "@" delimiter):
//
//	whoami            -> job "whoami", single group auto-resolved
//	whoami@web        -> job "whoami", group "web"
//	whoami@web@2      -> job "whoami", group "web", started at 2 replicas
func ParseName(name string, opts ParseOptions) (ParsedName, error) {
	if name == "" {
		return ParsedName{}, fmt.Errorf("instance name cannot be empty")
	}

	parts := strings.Split(name, opts.Delimiter)
	parsed := ParsedName{Original: name}

	switch len(parts) {
	case 1:
		parsed.JobID = parts[0]
	case 2:
		parsed.JobID = parts[0]
		parsed.Group = parts[1]
	case 3:
		parsed.JobID = parts[0]
		parsed.Group = parts[1]
		replicas, err := strconv.Atoi(parts[2])
		if err != nil {
			return ParsedName{}, fmt.Errorf("invalid replicas in name [%s]: %w", name, err)
		}
		if replicas < 0 {
			return ParsedName{}, fmt.Errorf("invalid replicas in name [%s]: must be >= 0", name)
		}
		parsed.Replicas = replicas
	default:
		return ParsedName{}, fmt.Errorf("invalid name [%s] should be: jobID, jobID%[2]sgroup, or jobID%[2]sgroup%[2]sreplicas", name, opts.Delimiter)
	}

	if parsed.JobID == "" {
		return ParsedName{}, fmt.Errorf("invalid name [%s]: job ID cannot be empty", name)
	}
	if len(parts) >= 2 && parsed.Group == "" {
		return ParsedName{}, fmt.Errorf("invalid name [%s]: task group cannot be empty", name)
	}

	return parsed, nil
}

// JobInstanceName builds the canonical instance name for a job's task group.
// A single-group job yields the bare job ID; a multi-group job yields
// "<jobID><delim><group>" so every group is addressable and unambiguous.
func JobInstanceName(jobID, group string, singleGroup bool, opts ParseOptions) string {
	if singleGroup {
		return jobID
	}
	return jobID + opts.Delimiter + group
}
