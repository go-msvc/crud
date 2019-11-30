package crud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-msvc/errors"
	"github.com/go-msvc/log"
	"github.com/go-msvc/store"
)

//New ...
func New() Crud {
	return Crud{
		stores: make([]store.IStore, 0),
	}
}

//Crud ...
type Crud struct {
	stores []store.IStore
}

//With another store
func (c Crud) With(s store.IStore) Crud {
	//todo: s.Name() name must be unique
	c.stores = append(c.stores, s)
	return c
}

//AddToMux ...
func (c Crud) AddToMux(mux *http.ServeMux) {
	for _, s := range c.stores {
		mux.Handle("/"+s.Name(), c.handler(c.post, c.get, s))
		mux.Handle("/"+s.Name()+"/", c.handler(c.post, c.get, s))
	}
}

func (c Crud) handler(
	postFunc func(s store.IStore, res http.ResponseWriter, req *http.Request),
	getFunc func(s store.IStore, res http.ResponseWriter, req *http.Request),
	s store.IStore,
) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		log.Debugf("HTTP %s %s", req.Method, req.URL.Path)
		switch req.Method {
		case http.MethodPost:
			postFunc(s, res, req)
			return
		case http.MethodGet:
			getFunc(s, res, req)
			return
		}
		http.Error(res, "CRUD: Create with POST, Read with GET, Update with PUT, Delete with DELETE.", http.StatusMethodNotAllowed)
		return
	} //handlerFunc()
} //Crud.handler()

//POST /item {...} to create a new item -> {"type":"<store.name>", "id":"<id>", "rev":<rev>, "ts":"<ts>", "user":"<user.id>"}
func (c Crud) post(s store.IStore, res http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/"+s.Name() {
		http.Error(res, fmt.Sprintf("Expecting POST /%s", s.Name()), http.StatusBadRequest)
		return
	}

	itemPtrValue := reflect.New(s.Type())
	if err := json.NewDecoder(req.Body).Decode(itemPtrValue.Interface()); err != nil {
		http.Error(res, fmt.Sprintf("Cannot parse body as JSON %s", s.Name()), http.StatusBadRequest)
		return
	}

	itemData := itemPtrValue.Elem().Interface()
	if itemValidator, ok := itemData.(IWithValidate); ok {
		if err := itemValidator.Validate(); err != nil {
			http.Error(res, errors.Wrapf(err, "invalid %s", s.Name()).Error(), http.StatusBadRequest)
			return
		}
	}

	info, err := s.Add(itemData)
	if err != nil {
		http.Error(res, errors.Wrapf(err, "failed to add").Error(), http.StatusInternalServerError)
		return
	}

	jsonValue, _ := json.Marshal(info)
	res.Header().Set("Item-ID", string(info.ID))
	res.Header().Set("Item-User-ID", string(info.UserID))
	res.Header().Set("Item-Timestamp", fmt.Sprintf("%s", info.Timestamp.Format(timestampFormat)))
	res.Header().Set("Item-Revision", fmt.Sprintf("%d", info.Rev))
	res.Header().Set("Content-Type", "application/json")
	res.Write(jsonValue)
}

//GET /item/id -> item data
func (c Crud) get(s store.IStore, res http.ResponseWriter, req *http.Request) {
	parts := strings.SplitN(req.URL.Path, "/", 4)
	if len(parts) != 3 || len(parts[2]) == 0 {
		http.Error(res, fmt.Sprintf("Expecting GET /%s/<id>", s.Name()), http.StatusNotFound)
		return
	}

	id := parts[2]
	v, info, err := s.Get(store.ID(id))
	if err != nil {
		http.Error(res, fmt.Sprintf("Expecting GET /%s/<id>", s.Name()), http.StatusNotFound)
		return
	}

	res.Header().Set("Item-Timestamp", fmt.Sprintf("%s", info.Timestamp.Format(timestampFormat)))
	res.Header().Set("Item-Revision", fmt.Sprintf("%d", info.Rev))
	res.Header().Set("Content-Type", "application/json")
	jsonValue, _ := json.Marshal(v)
	res.Write(jsonValue)
}

const timestampFormat = "2006-01-02 15:04:05-0700"

//IWithValidate ...
type IWithValidate interface {
	Validate() error
}
