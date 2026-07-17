package sablier

// DockerContainerInfo holds Docker-specific container metadata.
type DockerContainerInfo struct {
	ID     string            `json:"id" jsonschema:"description=Container ID (template access: .Docker.ID),example=a1b2c3d4e5f6"`
	Image  string            `json:"image" jsonschema:"description=Container image (template access: .Docker.Image),example=nginx:latest"`
	Labels map[string]string `json:"labels,omitempty" jsonschema:"description=Container labels (template access: .Docker.Labels)"`
}

// SwarmServiceInfo holds Docker Swarm-specific service metadata.
type SwarmServiceInfo struct {
	ID     string            `json:"id" jsonschema:"description=Service ID (template access: .Swarm.ID),example=a1b2c3d4e5f6"`
	Image  string            `json:"image" jsonschema:"description=Service image (template access: .Swarm.Image),example=nginx:latest"`
	Labels map[string]string `json:"labels,omitempty" jsonschema:"description=Service labels (template access: .Swarm.Labels)"`
}

// KubernetesWorkloadInfo holds Kubernetes-specific workload metadata.
type KubernetesWorkloadInfo struct {
	Namespace string            `json:"namespace" jsonschema:"description=Kubernetes namespace (template access: .Kubernetes.Namespace),example=default"`
	Kind      string            `json:"kind" jsonschema:"description=Workload kind (template access: .Kubernetes.Kind),example=Deployment"`
	Image     string            `json:"image" jsonschema:"description=Workload image (template access: .Kubernetes.Image),example=nginx:latest"`
	Labels    map[string]string `json:"labels,omitempty" jsonschema:"description=Workload labels (template access: .Kubernetes.Labels)"`
}

// PodmanContainerInfo holds Podman-specific container metadata.
type PodmanContainerInfo struct {
	ID     string            `json:"id" jsonschema:"description=Container ID (template access: .Podman.ID),example=a1b2c3d4e5f6"`
	Image  string            `json:"image" jsonschema:"description=Container image (template access: .Podman.Image),example=nginx:latest"`
	Labels map[string]string `json:"labels,omitempty" jsonschema:"description=Container labels (template access: .Podman.Labels)"`
}

// NomadJobInfo holds HashiCorp Nomad-specific workload metadata. A Sablier
// instance maps to a single task group of a Nomad job.
type NomadJobInfo struct {
	Namespace string            `json:"namespace" jsonschema:"description=Nomad namespace (template access: .Nomad.Namespace),example=default"`
	JobID     string            `json:"jobId" jsonschema:"description=Nomad job ID (template access: .Nomad.JobID),example=whoami"`
	Group     string            `json:"group" jsonschema:"description=Nomad task group (template access: .Nomad.Group),example=demo"`
	Image     string            `json:"image,omitempty" jsonschema:"description=First task image of the group (template access: .Nomad.Image),example=traefik/whoami:latest"`
	Meta      map[string]string `json:"meta,omitempty" jsonschema:"description=Merged job and group meta (template access: .Nomad.Meta)"`
}
