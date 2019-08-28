package auth

import (
  "gitlab.768bit.com/768bit/prush-web/backend/sessions"
  "gitlab.768bit.com/768bit/prush/store"
  "gitlab.768bit.com/768bit/prush/models"
  "gitlab.768bit.com/768bit/prush-web/shared/templatedata"
  "gitlab.768bit.com/768bit/prush-web/shared/wraprender"
  "github.com/768bit/isokit"
  "github.com/globalsign/mgo/bson"
  "github.com/gobuffalo/buffalo"
  "github.com/gobuffalo/buffalo/render"
  "github.com/pkg/errors"
  "golang.org/x/crypto/bcrypt"
  "log"
  "net/url"
  "strings"
)

type AuthMan struct {
  app         *buffalo.App
  adapter *store.Adapter
  templateSet *isokit.TemplateSet
  seshMan *sessions.Manager
}

func NewAuthManInstance(adapter *store.Adapter, authApp *buffalo.App, seshMan *sessions.Manager, ts *isokit.TemplateSet) *AuthMan {

  am := &AuthMan{
    adapter: adapter,
    app:         authApp,
    templateSet: ts,
    seshMan:seshMan,
  }

  am.bindAuthHandlers(am.app)

  return am

}

type LoginForm struct {
  Username string `form:"email"`
  PrevPage string `form:"prevPage"`
}

type LoginPasswordForm struct {
  Email    string `form:"email"`
  Password string `form:"password"`
  PrevPage string `form:"prevPage"`
}

func (authMan *AuthMan) bindAuthHandlers(authGroup *buffalo.App) {

  authGroup.GET("/logout", authMan.LogoutPageHandler)
  authGroup.GET("/login", authMan.LoginPageHandler)
  authGroup.GET("/ping", authMan.KeepAliveSessionHandler)
  //authGroup.GET("/login_pass", authMan.LoginPasswordPageHandler)
  authGroup.GET("/login_hop/{ticket_id}", authMan.LoginHopPageHandler)
  authGroup.POST("/login", authMan.LoginPostHandler)
  authGroup.POST("/login_pass", authMan.LoginPasswordPostHandler)
  authGroup.POST("/login_hop/{ticket_id}", authMan.LoginHopPostHandler)
  authGroup.POST("/_jwtAuth", authMan.JWTAuthHandler)

}

func (authMan *AuthMan) RedirectToLoginWithURI(c buffalo.Context, uri string) error {

  c.Session().Clear()

  c.Session().Save()

  c.Response().Header().Set("Location", "/_auth/login?req_path="+uri)
  return c.Redirect(302, "/_auth/login?req_path="+uri)

}

func (authMan *AuthMan) LogoutPageHandler(c buffalo.Context) error {

  c.Session().Clear()

  c.Cookies().Delete("prush_login")

  prevPage := "/"

  if m, ok := c.Params().(url.Values); ok {
    if p := m.Get("req_path"); p != "" {
      prevPage = p
    }
  }

  c.Response().Header().Set("Location", prevPage)
  return c.Redirect(302, prevPage)
}

func (authMan *AuthMan) LoginPageHandler(c buffalo.Context) error {

  c.Session().Clear()

  prevPage := "/"

  if m, ok := c.Params().(url.Values); ok {
    if p := m.Get("req_path"); p != "" {
      prevPage = p
    }
  }


  templateData := &templatedata.BasePageData{
    PageTitle:  "Login",
    RenderPage: "prush/views/login_content",
    PrevPage:   prevPage,
  }
  c.Set("page", templateData)
  p := &isokit.RenderParams{Writer: c.Response(), Data: templateData}
  return c.Render(200, wraprender.HTML(authMan.templateSet, "prush/views/blank_page", p))
}

func (authMan *AuthMan) LoginPostHandler(c buffalo.Context) error {

  loginPayload := &LoginForm{}

  if err := c.Bind(loginPayload); err != nil {

    return errors.WithStack(err)

  }

  email := strings.ToLower(loginPayload.Username)

  if coll, err := authMan.adapter.GetCollection("users"); err != nil {
    log.Println("User collection missing")
    return redirectBackToLoginWithError(c, "User doesn't exist")
  } else {
    curs := coll.RawCollection().Find(bson.M{ "document.email" : email })
    if n, err := curs.Count(); err != nil || n != 1 {
      log.Println("User missing. Email:", email, "Err:", err)
      return redirectBackToLoginWithError(c, "User doesn't exist")
    } else {
      var user models.User
      if err := curs.One(&user); err != nil {
        log.Println("User missing. Email:", email, "Err:", err)
        return redirectBackToLoginWithError(c, "User doesn't exist")
      } else {
        templateData := templatedata.NewLoginAuthPageData("Login", "prush/views/login_password_content", email)
        templateData.SetPrevPage(loginPayload.PrevPage)
        c.Set("page", templateData)
        p := &isokit.RenderParams{Writer: c.Response(), Data: templateData}
        return c.Render(200, wraprender.HTML(authMan.templateSet, "prush/views/blank_page", p))
      }
    }
  }

}

func redirectBackToLogin(c buffalo.Context) error {

  c.Session().Clear()

  c.Response().Header().Set("Location", "/_auth/login")
  return c.Redirect(302, "/_auth/login")

}

func redirectBackToLoginWithError(c buffalo.Context, errorMsg string) error {

  c.Session().Clear()

  c.Response().Header().Set("Location", "/_auth/login?login_error="+errorMsg)
  return c.Redirect(302, "/_auth/login?login_error="+errorMsg)

}

func redirectLocalLogin(username string, c buffalo.Context) error {

  c.Session().Set("username", username)

  c.Session().Set("login_method", "$_local")

  log.Print("Beginning Login Process for User " + username)

  c.Response().Header().Set("Location", "/_auth/login_pass")
  return c.Redirect(302, "/_auth/login_pass")

}

//func (authMan *AuthMan) LoginPasswordPageHandler(c buffalo.Context) error {
//
//  templateData := &templatedata.BasePageData{
//    PageTitle:  "Login",
//    RenderPage: "prush/views/login_password_content",
//  }
//  c.Set("page", templateData)
//  p := &isokit.RenderParams{Writer: c.Response(), Data: templateData}
//  return c.Render(200, wraprender.HTML(authMan.templateSet, "prush/views/blank_page", p))
//}

func (authMan *AuthMan) LoginPasswordPostHandler(c buffalo.Context) error {

  loginPwdPayload := &LoginPasswordForm{}

  if err := c.Bind(loginPwdPayload); err != nil {

    return errors.WithStack(err)

  }

  email := strings.ToLower(loginPwdPayload.Email)
  prevPage := loginPwdPayload.PrevPage
  if prevPage == "" || strings.HasPrefix(prevPage, "/_auth") {
    prevPage = "/"
  }

  if coll, err := authMan.adapter.GetCollection("users"); err != nil {
    return redirectBackToLoginWithError(c, "User doesn't exist")
  } else {
    curs := coll.RawCollection().Find(bson.M{ "document.email" : email })
    if n, err := curs.Count(); err != nil || n != 1 {
      return redirectBackToLoginWithError(c, "User doesn't exist")
    } else {
      var hdr models.Header
      if err := curs.One(&hdr); err != nil {
        return redirectBackToLoginWithError(c, "User doesn't exist")
      } else if user, err := models.UserFromHeader(&hdr); err != nil {
        log.Println(err)
        return redirectBackToLoginWithError(c, "User doesn't exist")
      } else {
        if verified, err := ComparePasswordWithHash(user.Password, loginPwdPayload.Password); err != nil {
          log.Println(err, verified, user.Password)
          return redirectBackToLoginWithError(c, "Error verifying Password")
        } else if !verified {
          return redirectBackToLoginWithError(c, "Incorrect Password")
        } else {
          //now create the session on the backend...
          if seshID, err := authMan.seshMan.NewUserSession(user); err != nil || seshID == "" {
            log.Println("Error Creating Session for", user.Email, err)
            return redirectBackToLoginWithError(c, "Unable to create session")
          } else {
            c.Session().Set("authenticated", true)
            c.Session().Set("userUUID", user.ID)
            c.Session().Set("sessionID", seshID)
            c.Session().Save()
            return redirectToMainPageURI(c, prevPage)
          }
        }
      }
    }
  }

}

func redirectToLoginHop(c buffalo.Context, hopTicket string) error {

  c.Response().Header().Set("Location", "/_auth/login_hop/"+hopTicket)
  return c.Redirect(302, "/_auth/login_hop/"+hopTicket)

}

func redirectToMainPageURI(c buffalo.Context, uri string) error {

  c.Response().Header().Set("Location", uri)
  return c.Redirect(302, uri)

}

func redirectToMainPage(c buffalo.Context) error {

  c.Response().Header().Set("Location", "/")
  return c.Redirect(302, "/")

}

type HopPageData struct {
  PageTitle  string
  RenderPage string
  HopTicket  string
}

func (authMan *AuthMan) LoginHopPageHandler(c buffalo.Context) error {

  templateData := &HopPageData{
    PageTitle:  "Login",
    RenderPage: "prush/views/login_hop_content",
    HopTicket:  c.Param("ticket_id"),
  }
  c.Set("page", templateData)
  p := &isokit.RenderParams{Writer: c.Response(), Data: templateData}
  return c.Render(200, wraprender.HTML(authMan.templateSet, "prush/views/blank_page", p))
}

type LoginHopClaim struct {
  Claim    bool   `json:"claim"`
  TicketID string `json:"ticket"`
}

func (authMan *AuthMan) LoginHopPostHandler(c buffalo.Context) error {

  hopClaimPayload := &LoginHopClaim{}

  if err := c.Bind(hopClaimPayload); err != nil {

    return errors.WithStack(err)

  }

  //need to now claim the ticket (do this once...)

  //ticketID := c.Param("ticket_id")

  if _, ok := c.Session().Get("vann_session_id").(string); ok {
    return redirectBackToLogin(c)
    //return authMan.claimSessionForUser(sessionID, ticketID, c)

  } else {

    return c.Render(200, render.JSON(map[string]interface{}{

      "claimed": false,
      "error":   "Unable to retrieve session ID. Please try again.",
    }))

  }

}

func (authMan *AuthMan) KeepAliveSessionHandler(c buffalo.Context) error {

  if _, ok := c.Session().Get("vann_session_id").(string); ok {
    return redirectBackToLogin(c)
    //return authMan.keepAliveSessionForUser(sessionID, c)

  } else {

    return c.Render(200, render.JSON(map[string]interface{}{
      "success": false,
      "error":   "Unable to keep-alive session for user.",
    }))

  }

}

//func (authMan *AuthMan) createSessionForUser(username string, c buffalo.Context) error {
//
//  log.Print("Processing Login for User " + username)
//
//  //get the user object so we can build the session... the user will only be able to claim a session for themselves once and this is done out of band...
//
//  ticketID := MakeUUIDString()
//
//  if started, sessionID := authMan.userMan.StartSessionForUser(username, ticketID); !started || sessionID == "" {
//
//    //unable to create a session so we need to redirect the user and cleanup session...
//
//    return redirectBackToLoginWithError(c, "Unable to start session on server, please try again.")
//
//  } else {
//
//    //session created redirect user hop...
//
//    c.Session().Set("vann_authenticated", false)
//
//    c.Session().Set("vann_session_id", sessionID)
//
//    log.Print("Issued Login Ticket " + ticketID)
//
//    return redirectToLoginHop(c, ticketID)
//
//  }
//
//}
//
//func (authMan *AuthMan) claimSessionForUser(sessionID string, ticketID string, c buffalo.Context) error {
//
//  log.Print("Claiming Session " + sessionID + " with ticket " + ticketID)
//
//  //get the user object so we can build the session... the user will only be able to claim a session for themselves once and this is done out of band...
//
//  if claimed := authMan.userMan.UseSessionTicketForUser(sessionID, ticketID); !claimed {
//
//    //ticket cannot be claimed so cannot generate the role tickets...
//
//    return c.Render(200, render.JSON(map[string]interface{}{
//
//      "claimed": false,
//      "error":   "Unable to claim session ID. Please try again.",
//    }))
//
//  } else {
//
//    //make role tickets...
//
//    c.Session().Set("vann_authenticated", true)
//
//    c.Session().Set("vann_ticket_id", ticketID)
//
//    return c.Render(200, render.JSON(map[string]interface{}{
//
//      "claimed":  true,
//      "redirect": "/en-gb/testing",
//    }))
//
//  }
//
//}
//
//func (authMan *AuthMan) getJWTTicketForUser(sessionID string, c buffalo.Context) error {
//
//  log.Print("Getting JWT Ticket for session " + sessionID)
//
//  //get the user object so we can build the session... the user will only be able to claim a session for themselves once and this is done out of band...
//
//  if jwtTicket, err := authMan.userMan.GetJWTTicketForUser(sessionID); err != nil {
//
//    //ticket cannot be claimed so cannot generate the role tickets...
//
//    return c.Render(500, render.String("Unable to obtain JWT Ticket."))
//
//  } else {
//
//    //make role tickets...
//
//    c.Session().Set("vann_jwt_ticket_id", jwtTicket)
//
//    return c.Render(200, render.String(jwtTicket))
//
//  }
//
//}
//
//func (authMan *AuthMan) keepAliveSessionForUser(sessionID string, c buffalo.Context) error {
//
//  log.Print("Keep-Alive Session " + sessionID)
//
//  //get the user object so we can build the session... the user will only be able to claim a session for themselves once and this is done out of band...
//
//  if keptAlive := authMan.userMan.KeepAliveSessionForUser(sessionID); !keptAlive {
//
//    //ticket cannot be claimed so cannot generate the role tickets...
//
//    return c.Render(200, render.JSON(map[string]interface{}{
//
//      "keptAlive": false,
//      "error":     "Unable to keep session alive.",
//    }))
//
//  } else {
//
//    return c.Render(200, render.JSON(map[string]interface{}{
//
//      "keptAlive": true,
//    }))
//
//  }
//
//}
//
func (authMan *AuthMan) JWTAuthHandler(c buffalo.Context) error {

 //we dont care about the payload - all we are doing is getting the JWTTicketID from the session as needed...

 if sessionID, ok := c.Session().Get("sessionID").(string); ok {

   if isAuth, ok := c.Session().Get("authenticated").(bool); ok && isAuth {

     if ticket, err := authMan.seshMan.GetJWTTicketForUser(sessionID); err != nil || ticket == nil {

       log.Println("Error getting JWTTicket", err, sessionID)

       return c.Render(500, render.String("Unable to obtain JWT Ticket."))

     } else {

       return c.Render(200, render.JSON(ticket))

     }

   } else {

     return c.Render(403, render.String("Unauthorised attempt to access JWT Ticket."))

   }

 } else {

   return c.Render(500, render.String("Unable to obtain JWT Ticket."))

 }

}




func HashAndSaltPassword(pwd string) (string, error) {

  bpwd := []byte(pwd)

  hash, err := bcrypt.GenerateFromPassword(bpwd, bcrypt.DefaultCost)

  if err != nil {

    return "", err

  } else {

    return string(hash), nil

  }

}

func ComparePasswordWithHash(hashedPwd string, pwd string) (bool, error) {

  bpwd := []byte(pwd)

  bhash := []byte(hashedPwd)

  err := bcrypt.CompareHashAndPassword(bhash, bpwd)

  if err != nil {

    return false, err

  } else {

    return true, nil

  }

}
