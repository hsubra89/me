# Hetzner Cloud For Personal Server Provisioning

`me` provisions the Personal Server on Hetzner Cloud using the official `github.com/hetznercloud/hcloud-go/v2/hcloud` client. Hetzner is the cloud boundary for this workflow: authentication is configured through `me auth hetzner`, while `me configure` uses the saved credentials to select Location and Server Type, create the server, and attach `me`-managed supporting resources.
