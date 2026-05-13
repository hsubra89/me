# Cloud-Init For Personal Server Bootstrap

`myn` bootstraps the Personal Server through cloud-init user data during server creation instead of creating a server first and then orchestrating setup over SSH. This makes first boot reproducible, lets Hetzner create root SSH access before polling begins, and keeps `myn configure` responsible for waiting until the bootstrap completion marker is available or the bootstrap timeout is reached.
