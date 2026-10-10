package amplifi

// Role is a unit's place in the mesh.
type Role string

// Unit roles.
const (
	RoleRouter    Role = "router"
	RoleMeshPoint Role = "mesh-point"
)
