package bufalus

import (
	"encoding/json"
	"fmt"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/plush"
	"github.com/gobuffalo/x/httpx"
	"net/http"
	"sort"
	"strings"
)

func productionErrorResponseFor(status int) []byte {

	if status == http.StatusForbidden {
		return []byte(prodUnauthorisedError)
	}

	if status == http.StatusNotFound {
		return []byte(prodNotFoundTmpl)
	}

	return []byte(prodErrorTmpl)
}

func defaultErrorHandler(status int, origErr error, c buffalo.Context) error {
	env := c.Value("env")

	c.Logger().Error(origErr)
	c.Response().WriteHeader(status)

	if env != nil && env.(string) == "production" {
		responseBody := productionErrorResponseFor(status)
		c.Response().Write(responseBody)
		return nil
	}

	msg := fmt.Sprintf("%+v", origErr)
	ct := httpx.ContentType(c.Request())
	switch strings.ToLower(ct) {
	case "application/json", "text/json", "json":
		err := json.NewEncoder(c.Response()).Encode(map[string]interface{}{
			"error": msg,
			"code":  status,
		})
		if err != nil {
			return err
		}
	case "application/xml", "text/xml", "xml":
	default:
		if err := c.Request().ParseForm(); err != nil {
			msg = fmt.Sprintf("%s\n%s", err.Error(), msg)
		}
		routes := c.Value("routes")
		odata := c.Data()
		delete(odata, "app")
		delete(odata, "routes")
		data := map[string]interface{}{
			"routes":      routes,
			"error":       msg,
			"status":      status,
			"data":        c.Data(),
			"params":      c.Params(),
			"posted_form": c.Request().Form,
			"context":     c,
			"headers":     inspectHeaders(c.Request().Header),
			"inspect": func(v interface{}) string {
				return fmt.Sprintf("%+v", v)
			},
		}
		ctx := plush.NewContextWith(data)
		t, err := plush.Render(devErrorTmpl, ctx)
		if err != nil {
			return err
		}
		res := c.Response()
		_, err = res.Write([]byte(t))
		return err
	}
	return nil
}

type inspectHeaders http.Header

func (i inspectHeaders) String() string {

	bb := make([]string, 0, len(i))

	for k, v := range i {
		bb = append(bb, fmt.Sprintf("%s: %s", k, v))
	}
	sort.Strings(bb)
	return strings.Join(bb, "\n\n")
}
