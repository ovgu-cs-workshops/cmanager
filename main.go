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
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ovgu-cs-workshops/cmanager/kubernetes"
	usersPackage "github.com/ovgu-cs-workshops/cmanager/users"

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
var pendingInstances map[string]chan struct{}

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

	if err := app.Client.Subscribe("wamp.registration.on_create", handleRegistration, nil); err != nil {
		util.Log.Errorf("Failed to subscribe to procedure registration: %s", err)
		os.Exit(service.ExitRegistration)
	}

	app.Run()

	os.Exit(service.ExitSuccess)
}

func handleRegistration(args wamp.List, _, _ wamp.Dict) {
	if len(args) < 2 {
		return
	}
	wid, ok := wamp.AsDict(args[1])
	if !ok {
		return
	}
	proc := wamp.OptionString(wid, "uri")
	if c, ok := pendingInstances[proc]; ok {
		close(c)
		delete(pendingInstances, proc)
	}
}

func waitForReadyPod(instance string, timeout time.Duration) error {
	readyChan := make(chan struct{})
	procedureName := fmt.Sprintf("rocks.git.tui.%s.create", instance)
	pendingInstances[procedureName] = readyChan
	select {
	case <-readyChan:
		return nil
	case <-time.After(timeout):
		delete(pendingInstances, procedureName)
		close(readyChan)
		return fmt.Errorf("Timeout waiting for instance %s", instance)
	}
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

		if err := waitForReadyPod(instanceID, 60*time.Second); err != nil {
			util.Log.Errorf("Failed to wait for pod to become ready: %v", err)
			return service.ReturnError("rocks.git.internal-error")
		}

		// Give the pod some time to settle down, initializing etc.
		time.Sleep(500 * time.Millisecond)

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
