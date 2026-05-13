# Hetzner Cloud For Personal Server Provisioning

`myn` provisions the Personal Server on Hetzner Cloud using the official `github.com/hetznercloud/hcloud-go/v2/hcloud` client. Hetzner is the cloud boundary for this workflow: authentication is configured through `myn auth hetzner`, while `myn configure` uses the saved credentials to select Location and Server Type, create the server, and attach `myn`-managed supporting resources.
