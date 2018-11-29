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
	"io"
	"os"
	"strconv"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	dockerctx "golang.org/x/net/context"

	"github.com/EmbeddedEnterprises/service"
	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"

	"github.com/ovgu-cs-workshops/cmanager/util"
)

type containerInfo struct {
	ticket      string
	containerID string
}

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

var users map[string]*containerInfo
var dockerClient *dockerclient.Client
var useNetwork bool

func checkLabels(ctr *types.Container) bool {
	_, ok := ctr.Labels["git-talk"]
	return ok
}

func ensureImagePulled() {
	out, err := dockerClient.ImagePull(dockerctx.Background(), os.Getenv("USER_IMAGE"), types.ImagePullOptions{})
	if err != nil {
		util.Log.Criticalf("Failed to pull image: %v", err)
		os.Exit(2)
	}
	defer out.Close()
	io.Copy(os.Stderr, out)
}

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
	} else {
		util.Log.Errorf("Failed to parse the value of 'USER_ALLOW_NETWORK': %v", err)
		os.Exit(service.ExitArgument)
	}

	users = map[string]*containerInfo{}
	dc, err := dockerclient.NewEnvClient()
	if err != nil {
		util.Log.Criticalf("Failed to connect to the docker daemon: %v", err)
		os.Exit(1)
	}
	dockerClient = dc
	ensureImagePulled()
	containers, err := dockerClient.ContainerList(dockerctx.Background(), types.ContainerListOptions{})
	if err != nil {
		util.Log.Warningf("Failed to read containers: %v", err)
	} else {
		for idx := range containers {
			ctr := &containers[idx]
			if !checkLabels(ctr) {
				continue
			}
			util.Log.Infof("Found container %s", ctr.ID[:16])
			username, uok := ctr.Labels["git-talk-user"]
			password, pok := ctr.Labels["git-talk-pass"]
			instance, iok := ctr.Labels["git-talk-inst"]
			if !uok || !pok || !iok {
				util.Log.Warningf("Failed to read user or pass, this should not happen!")
				continue
			}
			util.Log.Infof("Found instance %v for user %v", instance, username)
			users[username] = &containerInfo{
				ticket:      password,
				containerID: instance,
			}
		}
	}

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
		instanceID, err := randomHex(16)
		if err != nil {
			return service.ReturnError("rocks.git.internal-error")
		}

		networkConfig := &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{},
		}
		if useNetwork {
			networkConfig.EndpointsConfig["gittalk"] = &network.EndpointSettings{}
		}

		resp, err := dockerClient.ContainerCreate(dockerctx.Background(), &container.Config{
			Image: os.Getenv("USER_IMAGE"),
			Labels: map[string]string{
				"git-talk":      "yes",
				"git-talk-user": authid,
				"git-talk-pass": ticket,
				"git-talk-inst": instanceID,
			},
			User: "1000:1000",
			Env:  append(os.Environ(), "RUNUSER="+authid, "RUNINST="+instanceID), // FIXME
		}, nil, networkConfig, "")
		if err != nil {
			util.Log.Errorf("Failed to create container: %v", err)
			return service.ReturnError("rocks.git.internal-error")
		}
		util.Log.Debugf("Created docker container with ID %v", resp.ID[:16])
		if err := dockerClient.ContainerStart(dockerctx.Background(), resp.ID, types.ContainerStartOptions{}); err != nil {
			util.Log.Errorf("Failed to start container: %v", err)
			return service.ReturnError("rocks.git.internal-error")
		}
		util.Log.Debugf("Started container with ID %v", resp.ID[:16])
		user = &containerInfo{
			ticket:      ticket,
			containerID: instanceID,
		}

		users[authid] = user
	}
	if user.ticket != ticket {
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
		"containerid": user.containerID,
	})
}
