package crud

//IClient to access CRUD services
type IClient interface {
	Server() string //server address
	//Auth()...

	Add() error
	Get()
	Upd()
	Del()
}
