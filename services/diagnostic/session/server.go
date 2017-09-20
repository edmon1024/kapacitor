package session

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/influxdata/kapacitor/services/diagnostic/internal/log"
	"github.com/influxdata/kapacitor/services/httpd"
	"github.com/influxdata/kapacitor/uuid"
)

const (
	sessionsPath = "/sessions"
)

//type Diagnostic interface {
//}

type Service struct {
	//diag   Diagnostic
	routes []httpd.Route

	sessions     SessionsDAO
	HTTPDService interface {
		AddRoutes([]httpd.Route) error
	}

	ticker  *time.Ticker
	closing chan struct{}
}

func NewService() *Service {
	return &Service{
		sessions: &sessionKV{
			sessions: make(map[uuid.UUID]*Session),
		},
	}
}

// TODO: implement
func (s *Service) Close() error {
	s.closing <- struct{}{}
	s.ticker.Stop()
	return nil
}

func (s *Service) Open() error {
	ch := make(chan struct{}, 0)
	s.ticker = time.NewTicker(time.Second)

	s.routes = []httpd.Route{
		{
			Method:      "POST",
			Pattern:     sessionsPath,
			HandlerFunc: s.handleCreateSession,
		},
		{
			Method:      "GET",
			Pattern:     sessionsPath,
			HandlerFunc: s.handleSession,
		},
	}
	s.closing = ch

	if s.HTTPDService == nil {
		return errors.New("must set HTTPDService")
	}

	if err := s.HTTPDService.AddRoutes(s.routes); err != nil {
		return fmt.Errorf("failed to add routes: %v", err)
	}

	go func() {
		for {
			select {
			case <-s.ticker.C:
				if err := s.sessions.Prune(); err != nil {
					// TODO: log error
				}
			case <-ch:
				return
			}
		}
	}()
	return nil
}

func (s *Service) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	tags := []log.StringField{}

	for k, v := range params {
		if len(v) != 1 {
			httpd.HttpError(w, "query params cannot contain duplicate pairs", true, http.StatusBadRequest)
			return
		}

		tags = append(tags, *log.String(k, v[0]).(*log.StringField))
	}

	session := s.sessions.Create(tags)
	u := fmt.Sprintf("%s,%s?id=%s&page=%v", httpd.BasePath, sessionsPath, session.ID(), session.Page())

	header := w.Header()
	header.Add("Link", fmt.Sprintf("<%s>; rel=\"next\";", u))
	header.Add("Deadline", session.Deadline().UTC().String())
}

func (s *Service) handleSession(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	id := params.Get("id")
	if id == "" {
		httpd.HttpError(w, "missing id query param", true, http.StatusBadRequest)
		return
	}
	pageStr := params.Get("page")
	if pageStr == "" {
		httpd.HttpError(w, "missing page param", true, http.StatusBadRequest)
		return
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		// TODO(desa): add some context to this error
		httpd.HttpError(w, err.Error(), true, http.StatusBadRequest)
		return
	}

	session, err := s.sessions.Get(id)
	if err != nil {
		// TODO(desa): add some context to this error
		httpd.HttpError(w, err.Error(), true, http.StatusBadRequest)
		return
	}

	p, err := session.GetPage(page)
	if err != nil {
		// TODO(desa): add some context to this error
		httpd.HttpError(w, err.Error(), true, http.StatusBadRequest)
		return
	}

	// TODO: add byte buffer pool here
	buf := bytes.NewBuffer(nil)
	// TODO: add support for JSON and logfmt encoding
	for _, line := range p {
		line.WriteTo(buf)
	}

	u := fmt.Sprintf("%s,%s?id=%s&page=%v", httpd.BasePath, sessionsPath, session.ID(), session.Page())

	header := w.Header()
	header.Add("Link", fmt.Sprintf("<%s>; rel=\"next\";", u))
	header.Add("Deadline", session.Deadline().UTC().String())
	fmt.Println(header)

	w.WriteHeader(http.StatusOK)
	//w.Write(buf.Bytes())
	w.Write([]byte("yah"))

	return
}