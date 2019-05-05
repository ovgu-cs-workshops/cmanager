package users

type ContainerInfo struct {
	Ticket      string
	ContainerID string
}

type ContainerList map[string]*ContainerInfo
