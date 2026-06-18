You are working on homectl, a self-hosted app manager for Docker-based applications.

Right now it's in the early stages of development so all changes can be tested against the specified targets. Whenever you do any changes that could affect manager or agent functionality you should test those changes by deploying to the dev manager and agent targets.

# To deploy manager

use make dev-deploy-manager it is deployed on jatin@65.20.72.36

# To deploy agents

use make dev-deploy-agent it is deployed on the following machines:
- akira (192.168.1.101)
- strawberry (192.168.1.102)
- windranger (192.168.1.103)

You have ssh access to all 3 machines and passwordless sudo too.

This app is setup such that it always runs in a docker, those targets each have their docker compose files defined in ~/homectl-compose if you need to make any changes to the compose files you can do so there.

# Admin UI

The admin UI is behind a basic auth and the password is stored in the .env file of the manager under ADMIN_PASSWORD.


