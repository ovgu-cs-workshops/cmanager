# cmanager - Container Manager

This project contains one part of the backend infrastructure for the git-introduction talk.
The task of this program is to spawn containers with the 'userland' image for users when they connect to the backend.

The container image is configurable and should provide a simple to use shell.

## Configuration

This service can be configured entirely using environment variables.
It is based on the [service lib](https://github.com/EmbeddedEnterprises/service), please refer to its documentation
for how to configure the websocket connection.

Additional environment variables and their meaning are described below:

| Name | Type | Description |
| ---- | ---- | ----------- |
| `USER_IMAGE` | string | Docker image to use for spawned containers |

