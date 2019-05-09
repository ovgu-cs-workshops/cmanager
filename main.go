/* cmanager - container manager
 *
 * Copyright (C) 2018
 *     Martin Koppehel <martin@embedded.enterprises>,
 *
 */

package main

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ovgu-cs-workshops/cmanager/kubernetes"

	"github.com/EmbeddedEnterprises/service"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"

	"github.com/ovgu-cs-workshops/cmanager/util"
)

var useNetwork bool
var kubernetesConnector *kubernetes.KubernetesConnector
var readyRexexp = regexp.MustCompile(`rocks\.git\.tui\.(?P<ID>[a-zA-Z0-9]+)\.create`)

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

	stopChan := make(chan struct{})
	go func() {
		if err := kubernetesConnector.WatchPVC(stopChan); err != nil {
			util.Log.Errorf("Failed to watch PVC changes: %v", err)
			app.Client.Close()
		}
	}()
	go func() {
		if err := kubernetesConnector.WatchPod(stopChan); err != nil {
			util.Log.Errorf("Failed to watch Pod changes: %v", err)
			app.Client.Close()
		}
	}()
	app.Run()

	close(stopChan)
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
	if readyRexexp.MatchString(proc) {
		name := readyRexexp.FindStringSubmatch(proc)[1]
		go func() {
			time.Sleep(500 * time.Millisecond)
			// best effort
			util.App.Client.Publish(fmt.Sprintf("rocks.git.%s.state", name), nil, wamp.List{"ready"}, nil)
		}()
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
	if _, err := kubernetesConnector.StartEnvironment(authid, ticket, os.Getenv("USER_IMAGE")); err != nil {
		util.Log.Errorf("Failed to create pod: %v", err)
		return service.ReturnError("rocks.git.internal-error")
	}
	return service.ReturnEmpty()
}

func getRoles(_ context.Context, args wamp.List, _, _ wamp.Dict) *client.InvokeResult {
	authid, idok := wamp.AsString(args[1])
	if !idok {
		return service.ReturnError("rocks.git.invalid-argument")
	}

	_, instance, ready, ok := kubernetesConnector.FindPodForUser(authid, nil)
	if !ok {
		return service.ReturnError("rocks.git.not-authorized")
	}

	return service.ReturnValue(wamp.Dict{
		"authroles":   wamp.List{"user"},
		"containerid": instance,
		"ready":       ready,
	})
}
