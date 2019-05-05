/* cmanager - container manager
 *
 * Copyright (C) 2018
 *     Martin Koppehel <martin@embedded.enterprises>,
 *
 */

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"github.com/ovgu-cs-workshops/cmanager/kubernetes"
	usersPackage "github.com/ovgu-cs-workshops/cmanager/users"
	"os"
	"strconv"
	"strings"

	"github.com/EmbeddedEnterprises/service"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"

	"github.com/ovgu-cs-workshops/cmanager/util"
)

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

var users usersPackage.ContainerList
var useNetwork bool
var kubernetesConnector *kubernetes.KubernetesConnector

func main() {
	app := service.New(service.Config{
		Name:          "cmanager",
		Serialization: client.MSGPACK,
		Version:       "0.1.0",
		Description:   "Service to authenticate users and manage docker containers",
	})
	util.Log = app.Logger
	util.App = app

	if allowNet, ok := os.LookupEnv("USER_ALLOW_NETWORK"); !ok {
		useNetwork = false
	} else if val, err := strconv.ParseBool(allowNet); err == nil {
		useNetwork = val
		if net, ok := os.LookupEnv("USER_NETWORK"); val && (!ok || len(strings.TrimSpace(net)) == 0) {
			util.Log.Errorf("This service requires the 'USER_NETWORK' environment variable to be set.")
			os.Exit(service.ExitArgument)
		}
	} else {
		util.Log.Errorf("Failed to parse the value of 'USER_ALLOW_NETWORK': %v", err)
		os.Exit(service.ExitArgument)
	}

	// Trying to get access to kubernetes cluster
	kubernetesConnector = kubernetes.New()
	users = kubernetesConnector.ExistingUsers()

	app.Connect()

	procedures := map[string]service.HandlerRegistration{
		"rocks.git.public.authenticate": service.HandlerRegistration{
			Handler: authenticate,
			Options: wamp.Dict{},
		},
		"rocks.git.public.get-roles": service.HandlerRegistration{
			Handler: getRoles,
			Options: wamp.Dict{},
		},
	}

	if err := app.RegisterAll(procedures); err != nil {
		util.Log.Errorf("Failed to register procedure: %s", err)
		os.Exit(service.ExitRegistration)
	}

	app.Run()

	os.Exit(service.ExitSuccess)
}

func authenticate(_ context.Context, args wamp.List, _, _ wamp.Dict) *client.InvokeResult {
	authid, idok := wamp.AsString(args[1])
	ticketObj, ticketok := wamp.AsDict(args[2])
	if !idok || !ticketok {
		return service.ReturnError("rocks.git.invalid-argument")
	}
	ticket, ticketok := wamp.AsString(ticketObj["ticket"])
	if !ticketok {
		return service.ReturnError("rocks.git.invalid-argument")
	}
	hashBytes := sha512.Sum512([]byte(ticket))
	ticket = hex.EncodeToString(hashBytes[:])
	user, ok := users[authid]
	if !ok {
		instanceID, err := randomHex(4)
		if err != nil {
			return service.ReturnError("rocks.git.internal-error")
		}

		_, err = kubernetesConnector.CreatePod(instanceID, authid, ticket, os.Getenv("USER_IMAGE"))
		if err != nil {
			util.Log.Errorf("Failed to create pod: %v", err)
			return service.ReturnError("rocks.git.internal-error")
		}

		user = &usersPackage.ContainerInfo{
			Ticket:      ticket,
			ContainerID: instanceID,
		}
		users[authid] = user
	}
	if user.Ticket != ticket {
		return service.ReturnError("rocks.git.invalid-password")
	}

	return service.ReturnEmpty()
}

func getRoles(_ context.Context, args wamp.List, _, _ wamp.Dict) *client.InvokeResult {
	authid, idok := wamp.AsString(args[1])
	user, userok := users[authid]
	if !idok || !userok {
		return service.ReturnError("rocks.git.invalid-argument")
	}
	return service.ReturnValue(wamp.Dict{
		"authroles":   wamp.List{"user"},
		"containerid": user.ContainerID,
	})
}
