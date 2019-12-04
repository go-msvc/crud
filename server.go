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
func New() Server {
	return Server{
		stores: make([]store.IStore, 0),
		opers:  make(map[string]operInfo),
	}
}

//Server ...
type Server struct {
	stores []store.IStore
	opers  map[string]operInfo
}

//With another store
func (server Server) With(s store.IStore) Server {
	//todo: s.Name() name must be unique
	server.stores = append(server.stores, s)
	return server
}

//WithOper adds a custom operation
func (server Server) WithOper(path string, oper IOper) Server {
	//validate the operation to have a Process() method
	operType := reflect.TypeOf(oper)
	operProcessMethod, ok := operType.MethodByName("Process")
	if !ok {
		panic(errors.Errorf("%T does not have Process(request)->(response,error) method", oper))
	}
	if operProcessMethod.Type.NumIn() != 2 {
		panic(errors.Errorf("%T.Process() does not have prototype Process(request)->(response,error)", oper))
	}
	operRequestType := operProcessMethod.Type.In(1)
	if err := store.ValidateUserType(operRequestType); err != nil {
		panic(errors.Wrapf(err, "invalid oper request %s used as arg %T.Process(<request>)", operRequestType.Name(), oper))
	}
	server.opers[path] = operInfo{
		oper:          oper,
		processMethod: reflect.ValueOf(oper).MethodByName("Process"), //of value, not of type as above :-)
		requestType:   operRequestType,
	}
	return server
}

//AddToMux ...
func (server Server) AddToMux(mux *http.ServeMux) {
	for _, s := range server.stores {
		mux.Handle("/"+s.Name(), server.storeHandler(server.storePost, server.storeGet, s))
		mux.Handle("/"+s.Name()+"/", server.storeHandler(server.storePost, server.storeGet, s))
	}
	for operPath, operInfo := range server.opers {
		mux.Handle(operPath, server.operHandler(server.operPost, operInfo))
	}
}

func (server Server) storeHandler(
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
		} //switch(method)
		http.Error(res, "CRUD: Create with POST, Read with GET, Update with PUT, Delete with DELETE.", http.StatusMethodNotAllowed)
		return
	} //handlerFunc()
} //Server.storeHandler()

//POST /item {...} to create a new item -> {"type":"<store.name>", "id":"<id>", "rev":<rev>, "ts":"<ts>", "user":"<user.id>"}
func (server Server) storePost(s store.IStore, res http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/"+s.Name() {
		http.Error(res, fmt.Sprintf("Expecting POST /%s", s.Name()), http.StatusBadRequest)
		return
	}

	itemPtrValue := reflect.New(s.Type())
	if err := json.NewDecoder(req.Body).Decode(itemPtrValue.Interface()); err != nil {
		http.Error(res, fmt.Sprintf("Cannot parse body as JSON %s", s.Name()), http.StatusBadRequest)
		return
	}

	itemDataPtr := itemPtrValue.Interface()
	if itemValidator, ok := itemDataPtr.(IWithValidate); ok {
		//call validate with pointer receiver
		if err := itemValidator.Validate(); err != nil {
			http.Error(res, errors.Wrapf(err, "invalid %s", s.Name()).Error(), http.StatusBadRequest)
			return
		}
	}

	itemData := itemPtrValue.Elem().Interface()
	if itemValidator, ok := itemData.(IWithValidate); ok {
		//call validate with const receiver
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
} //Server.storePost()

//GET /item/id -> item data
func (server Server) storeGet(s store.IStore, res http.ResponseWriter, req *http.Request) {
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
} //server.storeGet()

type operInfo struct {
	oper          IOper
	processMethod reflect.Value
	requestType   reflect.Type
}

func (server Server) operHandler(
	postFunc func(o operInfo, res http.ResponseWriter, req *http.Request),
	o operInfo,
) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		log.Debugf("HTTP %s %s", req.Method, req.URL.Path)
		switch req.Method {
		case http.MethodPost:
			postFunc(o, res, req)
			return
		} //switch(method)
		http.Error(res, req.URL.Path+" accepts only POST", http.StatusMethodNotAllowed)
		return
	} //handlerFunc()
} //Server.operHandler()

//POST /path {...} to call the operation
func (server Server) operPost(o operInfo, res http.ResponseWriter, req *http.Request) {
	requestPtrValue := reflect.New(o.requestType)
	if err := json.NewDecoder(req.Body).Decode(requestPtrValue.Interface()); err != nil {
		http.Error(res, fmt.Sprintf("Cannot parse body as JSON %s", o.requestType.Name()), http.StatusBadRequest)
		return
	}

	requestDataPtr := requestPtrValue.Interface()
	if requestValidator, ok := requestDataPtr.(IWithValidate); ok {
		//call validate with pointer receiver
		if err := requestValidator.Validate(); err != nil {
			http.Error(res, errors.Wrapf(err, "invalid %s", o.requestType.Name()).Error(), http.StatusBadRequest)
			return
		}
	}

	requestData := requestPtrValue.Elem().Interface()
	if requestValidator, ok := requestData.(IWithValidate); ok {
		//call validate with const receiver
		if err := requestValidator.Validate(); err != nil {
			http.Error(res, errors.Wrapf(err, "invalid %s", o.requestType.Name()).Error(), http.StatusBadRequest)
			return
		}
	}

	//call the oper.Process() method to make the response
	in := make([]reflect.Value, 0)
	//in = append(in, ...) //receiver
	in = append(in, reflect.ValueOf(requestData))
	out := o.processMethod.Call(in)
	if len(out) != 2 {
		http.Error(res, errors.Errorf("%T.Process() returned %d instead of %d values", o.oper, len(out), 2).Error(), http.StatusInternalServerError)
		return
	}
	responseData := out[0].Interface()
	var err error
	if out[1].Interface() != nil {
		var ok bool
		err, ok = out[1].Interface().(error)
		if !ok {
			panic(errors.Errorf("%T:%v is not error", out[1], out[1]))
		}
		http.Error(res, errors.Errorf("failed: %v", err).Error(), http.StatusBadRequest)
		return
	}
	jsonValue, _ := json.Marshal(responseData)
	res.Header().Set("Content-Type", "application/json")
	res.Write(jsonValue)
} //Server.operPost()

const timestampFormat = "2006-01-02 15:04:05-0700"

//IWithValidate ...
type IWithValidate interface {
	Validate() error
}
