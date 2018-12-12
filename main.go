package bufalus

import (
	"fmt"
	"github.com/768bit/packr"
	"github.com/gobuffalo/buffalo"
	"github.com/gorilla/sessions"
	"gitlab.768bit.com/vann/libvann/bufalus/websocket/wsserver"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

//BUFALUS is the HTTP server component for Vann and will serve either a module
// (over http but requests are proxied from a root server(s)) or the root application host proxies....

type BuffaloApp = buffalo.App

type Options struct {
	Env          string
	RootFolder   string
	PublicFolder string
	SessionName  string
	SessionKey   string
	BindAddress  string
	Port         int
}

type App struct {
	*BuffaloApp
	rootFolder          string
	primaryPublicFolder string
}

func New(opts *Options) *App {

	cwd, err := os.Getwd()

	if err != nil {
		log.Print("Unable to get Current Working Directory: ", err)
	}

	cwd = filepath.Clean(cwd)

	buffaloEnv := "production"

	if opts.Env == "development" || opts.Env == "development_build" || opts.Env == "production_build" || opts.Env == "test" || opts.Env == "testing" {
		if opts.Env == "test" || opts.Env == "testing" {
			buffaloEnv = "testing"
		} else {
			buffaloEnv = "development"
		}
	}

	log.Printf("Starting Bufalus HTTP Server in %s mode in CWD %s", opts.Env, cwd)

	buffaloOpts := buffalo.Options{
		Env:          buffaloEnv,
		SessionName:  opts.SessionName,
		SessionStore: sessions.NewCookieStore([]byte(opts.SessionKey)),
		Addr:         fmt.Sprintf("%s:%d", opts.BindAddress, opts.Port),
	}

	ba := &App{
		BuffaloApp:          buffalo.New(buffaloOpts),
		rootFolder:          opts.RootFolder,
		primaryPublicFolder: opts.PublicFolder,
	}

	if ba.primaryPublicFolder != "" {
		ba.ServePublic("/_public", opts.PublicFolder)
	}

	log.Print("======== BUFALUS READY TO SERVE ========")

	return ba

}

func (ba *App) ServePublic(rootPath string, folderPath string) {

	ba.ServeFiles(rootPath, http.Dir(folderPath))

}

func (ba *App) ServeAssets(rootPath string, box *packr.Box) {

	ba.ServeFiles(rootPath, box)

}

func (ba *App) ServeWebsocket(rootPath string, wss *wsserver.WebSocketServer) {

	ba.GET(rootPath, wss.ServeWS())

}
