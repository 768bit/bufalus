package sessions

import (
  "gitlab.768bit.com/768bit/prush/models"
  "fmt"
  "github.com/768bit/vutils"
  "github.com/globalsign/mgo"
  "github.com/globalsign/mgo/bson"
  "log"
  "time"
)

var DEFAULT_SESSION_TIMEOUT, _ = time.ParseDuration(fmt.Sprintf("%dm", 15))

func NewSessionManager(sessionColl *mgo.Collection) *Manager {
  return &Manager{
    sessionColl:sessionColl,
  }
}

type Manager struct {
  sessionColl *mgo.Collection
}

func (sm *Manager) NewUserSession(user *models.User) (string, error) {

  seshID, _ := vutils.UUID.MakeUUIDStringNoDashes()
  jwtTicketID, _ := vutils.UUID.MakeUUIDStringNoDashes()

  roleList := []string{}

  for role, enabled := range user.RoleMap {

    if enabled {
      roleList = append(roleList, role)
    }

  }

  sesh := SessionModel{
    ID: seshID,
    JWTTicketID:jwtTicketID,
    Validated:false,
    ValidatedJWT:false,
    UserUUID:user.ID,
    Email:user.Email,
    FullName:user.Name,
    LastSeen:time.Now(),
    Roles:roleList,
  }

  sesh.Expires = sesh.LastSeen.Add(DEFAULT_SESSION_TIMEOUT)

  if err := sm.sessionColl.Insert(&sesh); err != nil {
    return "", err
  }

  return seshID, nil

}

func (sm *Manager) DestroyUserSession(sessionID string) (bool, error) {

  //create a new session object for the user - it will expire but if the user
  qry := bson.M{
    "id": sessionID,
  }

  if err := sm.sessionColl.Remove(qry); err != nil {

    return false, err

  } else {

    return true, nil

  }

}

type JWTTicket struct {
  UserUUID string `json:"userUUID"`
  JWTTicketID string `json:"jwtTicketID"`
}

func (sm *Manager) GetJWTTicketForUser(sessionID string) (*JWTTicket, error) {

  sesh := SessionModel{}

  qry := bson.M{
    "id":    sessionID,
  }

  res := sm.sessionColl.Find(qry)

  if err := res.One(&sesh); err != nil {

    return nil, err

  } else if sesh.JWTTicketID != "" {

    if sesh.Expires.Before(time.Now()) {
      return nil, nil
    }

    log.Println("Obtained JWTTicket")

    return &JWTTicket{
      UserUUID:sesh.UserUUID,
      JWTTicketID:sesh.JWTTicketID,
    }, nil

  } else {

    return nil, nil

  }

}

func (sm *Manager) UseJWTTicketForUser(userUUID string, jwtTicketID string) (bool, string) {

  sesh := SessionModel{}

  qry := bson.M{
    "userUUID":     userUUID,
    "jwtTicketID":  jwtTicketID,
  }

  res := sm.sessionColl.Find(qry)

  if err := res.One(&sesh); err != nil {

    return false, ""

  } else {

    nt := time.Now()

    if sesh.Expires.Before(nt) {
      return false, ""
    }

    if uerr := sm.sessionColl.Update(qry, bson.M{
      "$set": bson.M{
        "expires": nt.Add(DEFAULT_SESSION_TIMEOUT),
        "lastSeen": nt,
        "validatedJWT": true,
      },
    }); uerr != nil {

      return false, ""

    }

    return true, sesh.ID

  }

}


func (sm *Manager) GetSessionForUser(sessionID string, ticketID string) *SessionModel {

  if isAlive := sm.SetKeepAliveSessionForUser(sessionID); isAlive {

    sesh := SessionModel{}

    qry := bson.M{
      "id": sessionID,
    }

    res := sm.sessionColl.Find(qry)

    if err := res.One(&sesh); err != nil {

      return nil

    } else {

      return &sesh

    }

  } else {

    return nil

  }

}

func (sm *Manager) GetJWTSessionForUser(sessionID string, jwtTicketID string) *SessionModel {

  sesh := SessionModel{}

  qry := bson.M{
    "id":    sessionID,
    "jwtTicketID":  jwtTicketID,
    "validatedJWT": true,
  }

  res := sm.sessionColl.Find(qry)

  if err := res.One(&sesh); err != nil {

    return nil

  } else {

    return &sesh

  }

}


func (sm *Manager) SetKeepAliveSessionForUser(sessionID string) bool {

  sesh := SessionModel{}

  qry := bson.M{
    "id": sessionID,
  }

  res := sm.sessionColl.Find(qry)

  if err := res.One(&sesh); err != nil {

    return false

  } else {

    nt := time.Now()

    if sesh.Expires.Before(nt) {
      return false
    }

    if uerr := sm.sessionColl.Update(qry, bson.M{
      "$set": bson.M{
        "lastSeen": nt,
        "expires": nt.Add(DEFAULT_SESSION_TIMEOUT),
      },
    }); uerr != nil {

      return false

    }

    return true

  }

}
