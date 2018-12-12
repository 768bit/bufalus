package wsserver

type IUserStoreProvider interface {
	UseJWTTicketForUser(userUUID string, jwtTicketID string) (bool, string)
}

type IAuthProvider interface {
}
