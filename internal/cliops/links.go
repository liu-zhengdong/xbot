package cliops

import "context"

const LinksMaxRequest = 1 << 20
const LinksMaxReply = 64 << 10
const linksOutputBudget = 32 << 10

// LinksSearch keeps the original catalog query; an empty query lists all.
type LinksSearch struct {
	Query string `json:"query,omitempty"`
}

// LinksLoad carries the original id or name; the domain resolves aliases.
type LinksLoad struct {
	ID string `json:"id"`
}

// LinksInstall carries one catalog id, name, or store connector to install.
type LinksInstall struct {
	ID string `json:"id"`
}

// LinksRemove carries one catalog id, name, or store connector to remove.
type LinksRemove struct {
	ID string `json:"id"`
}

// LinksReply contains only the original text and the actual error.
type LinksReply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

type LinksHandlers struct {
	Search  func(context.Context, LinksSearch) (LinksReply, error)
	Load    func(context.Context, LinksLoad) (LinksReply, error)
	Install func(context.Context, LinksInstall) (LinksReply, error)
	Remove  func(context.Context, LinksRemove) (LinksReply, error)
}
