package models

import (
	"errors"
	"strings"
)

// AppRef identifies an Argo CD Application by the namespace its resource lives in and its name.
// With apps-in-any-namespace, team-a:web and team-b:web are different Applications, so a name
// alone never identifies one.
type AppRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (a AppRef) String() string { return a.Namespace + ":" + a.Name }

// ParseAppRef parses "<namespace>:<name>", the format of Argo CD's Argocd-Application-Name header.
func ParseAppRef(s string) (AppRef, error) {
	ns, name, ok := strings.Cut(s, ":")
	if !ok || ns == "" || name == "" || strings.Contains(name, ":") {
		return AppRef{}, errors.New("expected <namespace>:<name>")
	}
	return AppRef{Namespace: ns, Name: name}, nil
}
