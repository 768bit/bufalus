package apiutils

import (
  "gitlab.768bit.com/768bit/prush"
  "gitlab.768bit.com/768bit/prush-web/backend/sessions"
  "gitlab.768bit.com/768bit/prush/store"
  "gitlab.768bit.com/768bit/prush/models"
  "github.com/768bit/go_wsutils"
  "strings"
)

type IAPICallable interface {
  GetHandler() APICallableHandler
  GetRoles() []string
  GetPermissions() *models.Permission
}


type APICallableHandler = func(req *go_wsutils.WebSocketRequestBody, prushServer *prush.Server, storeAdapter *store.Adapter, session *sessions.SessionModel, payload interface{}, options interface{}) (interface{}, error)

type APICallable struct {

  RequiredRoles       []string
  RequiredPermissions *models.Permission
  Handler             APICallableHandler

}

func (ap *APICallable) GetHandler() APICallableHandler {

  return ap.Handler

}

func (ap *APICallable) GetRoles() []string {

  return ap.RequiredRoles

}

func (ap *APICallable) GetPermissions() *models.Permission {

  return ap.RequiredPermissions

}

type IRestAPICallable interface {
  GetAPICallable(callType string) IAPICallable
}

type RestAPICallable struct {
  Create  IAPICallable
  Read    IAPICallable
  Update  IAPICallable
  Destroy IAPICallable
}

func (rpcAP *RestAPICallable) GetAPICallable(callType string) IAPICallable {

  switch strings.ToLower(callType) {
  case "create":
    return rpcAP.Create
  case "read":
    return rpcAP.Read
  case "update":
    return rpcAP.Update
  case "destroy":
    return rpcAP.Destroy
  default:
    return nil
  }

}
