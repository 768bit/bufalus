package sessions

import "time"

type SessionModel struct {

  ID           string    `json:"id" bson:"id"`
  JWTTicketID  string    `json:"jwtTicketID" bson:"jwtTicketID"`
  Validated    bool      `json:"validated" bson:"validated"`
  ValidatedJWT bool      `json:"validatedJWT" bson:"validatedJWT"`
  LastSeen     time.Time `json:"lastSeen" bson:"lastSeen"`
  UserUUID     string    `json:"userUUID" bson:"userUUID"`
  Email        string    `json:"email" bson:"email"`
  FullName     string    `json:"fullName" bson:"fullName"`
  Roles        []string  `json:"roles" bson:"roles"`
  Expires      time.Time `json:"expires" bson:"expires"`

}


func (sesh *SessionModel) ContainsRole(role string) bool {
  set := make(map[string]bool, len(sesh.Roles))
  for _, s := range sesh.Roles {
    set[s] = true
  }

  val, ok := set[role]
  return ok && val
}
