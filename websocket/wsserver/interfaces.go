package wsserver

type ISessionProvider interface {
	UseJWTTicketForUser(userUUID string, jwtTicketID string) (bool, string)
}

type IAuthProvider interface {
}
