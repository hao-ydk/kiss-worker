# KISS-WORKER

English | [简体中文](README.md)

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/fishjar/kiss-worker)

A data synchronization service for [KISS-Translator](https://github.com/fishjar/kiss-translator). It can run on Cloudflare Workers or be self-hosted with Docker.

The server remains compatible with existing clients: `POST /sync` synchronizes settings, rules, and vocabulary, while `GET /rules?psk=...` serves shared rules. The Cloudflare implementation uses Durable Objects to order concurrent writes for the same data key and lazily migrates existing records from KV on first access.

## Choose a deployment method

There are four parallel Cloudflare deployment methods. Choose the first suitable option from this table:

| Method                                                             | Best for                                 | Command line required | Automatic Git deployments                 |
| ------------------------------------------------------------------ | ---------------------------------------- | --------------------- | ----------------------------------------- |
| [One-click deployment](#method-1-one-click-deployment-recommended) | New users who want the fewest steps      | No                    | Yes; Cloudflare creates a repository copy |
| [Import a GitHub repository](#method-2-import-a-github-repository) | Users who want to maintain a fork        | No                    | Yes                                       |
| [GitHub Actions](#method-3-github-actions-deployment)              | Users who want to manage their own CI/CD | No                    | Yes                                       |
| [Command-line deployment](#method-4-command-line-deployment)       | Development, debugging, or customization | Yes                   | No                                        |

All four Cloudflare methods create or bind the required KV and Durable Object resources. You do not need to create a KV namespace or paste a namespace ID manually. “Import a GitHub repository” runs through Cloudflare Workers Builds, while “GitHub Actions” runs on a GitHub-hosted runner; they are independent options. For self-hosting, see the separate [Docker section](#docker-self-hosting).

## Before you deploy

- A [Cloudflare](https://dash.cloudflare.com/) account.
- A long, unpredictable synchronization password stored as `AUTH_VALUE` during deployment. You will enter this same password in KISS-Translator. It is not a `CF_API_TOKEN`.
- A custom domain is optional; you can use the Cloudflare-provided `workers.dev` address.

## Method 1: One-click deployment (recommended)

This is the simplest option for a new installation. It does not require downloading the source code or installing Node.js.

1. Click the button and sign in to Cloudflare:

   [![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/fishjar/kiss-worker)

2. Follow the prompts to authorize GitHub and confirm the repository copy and Worker names.
3. Enter your synchronization password for `AUTH_VALUE`, then start the deployment.
4. Wait for Workers Builds to finish. Cloudflare configures KV and the Durable Object from `wrangler.toml`.
5. Open the new Worker and copy its `https://<worker-name>.<account-subdomain>.workers.dev` address from the overview or “Settings → Domains & Routes”.

The one-click button is intended mainly for new installations. To update an existing Worker, follow the [in-place upgrade instructions](#upgrade-an-existing-worker-in-place) or use the command line so that you do not accidentally create an empty, separate deployment.

See the official [Deploy to Cloudflare button documentation](https://developers.cloudflare.com/workers/platform/deploy-buttons/).

## Method 2: Import a GitHub repository

This method requires no local command line and is useful when you want Git pushes to update the Worker automatically.

1. [Fork this repository](https://github.com/fishjar/kiss-worker/fork) on GitHub.
2. In the Cloudflare Dashboard, open “Workers & Pages → Create application → Import a repository”.
3. Connect GitHub and select your fork.
4. Confirm these build settings:

   | Setting           | Value            |
   | ----------------- | ---------------- |
   | Production branch | `master`         |
   | Root directory    | `/`              |
   | Build command     | `npm run build`  |
   | Deploy command    | `npm run deploy` |

5. Save and deploy. Cloudflare installs the dependencies and configures KV and the Durable Object automatically.
6. Immediately after the initial deployment, open “Settings → Variables & Secrets → Add” for the Worker. Add `AUTH_VALUE` as a runtime **Secret**, then redeploy or retry the failed deployment.

Do not add `AUTH_VALUE` only as a Workers Builds build variable. Build variables exist only while building and are unavailable to the running Worker. Once configured, pushes to the fork's `master` branch trigger future builds and deployments.

See [Workers Builds](https://developers.cloudflare.com/workers/ci-cd/builds/) and its [build configuration reference](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/).

## Method 3: GitHub Actions deployment

This method uses the workflow included in your fork to test, build, and deploy the Worker. It does not require Node.js on your computer. It is independent of Cloudflare Workers Builds in the previous section. Do not enable both CI systems for the same Worker, or one commit may trigger two deployments.

1. [Fork this repository](https://github.com/fishjar/kiss-worker/fork) on GitHub.
2. Open the fork's “Actions” page and enable workflows.
3. Copy the target account's Account ID from the Cloudflare Dashboard.
4. Create an API Token from Cloudflare's “Account API Tokens” page. Grant Workers edit permission and restrict its account and zone resources to the actual deployment scope.
5. Open “Settings → Secrets and variables → Actions” in the fork and add these Repository secrets:

   | Secret          | Value                                  |
   | --------------- | -------------------------------------- |
   | `CF_ACCOUNT_ID` | Cloudflare Account ID                  |
   | `CF_API_TOKEN`  | The Cloudflare API Token created above |

6. Open “Actions → Cloudflare Worker → Run workflow”, select `master`, and run the first deployment. Forking alone does not create a new push event, so the first run must be triggered manually.
7. After deployment, open “Settings → Variables & Secrets → Add” for the Cloudflare Worker. Add `AUTH_VALUE` as a runtime **Secret**, then redeploy.
8. Future pushes to the fork's `master` branch automatically run `npm ci`, the tests, the dry-run build, and the production deployment.

`CF_ACCOUNT_ID` and `CF_API_TOKEN` allow GitHub Actions to deploy code to Cloudflare. `AUTH_VALUE` is the synchronization password used by KISS-Translator. These values serve different purposes and are not interchangeable. If either GitHub Secret is absent, the workflow still tests and builds the project but safely skips the Deploy step.

See the official [Cloudflare GitHub Actions documentation](https://developers.cloudflare.com/workers/ci-cd/external-cicd/github-actions/).

## Method 4: Command-line deployment

### Prerequisites

- [Git](https://git-scm.com/)
- Node.js 22 or newer

### Deployment

```sh
git clone https://github.com/fishjar/kiss-worker.git
cd kiss-worker
npm ci
npx wrangler login
npm run deploy
npm run secret
```

The first `npx wrangler login` opens a Cloudflare authorization page. `npm run deploy` configures KV and the Durable Object automatically. `npm run secret` prompts for `AUTH_VALUE` without writing it to the repository. Wrangler creates a new Worker version after the secret is saved.

For local development, copy `.dev.vars.example` to `.dev.vars`, replace the sample password, and run:

```sh
npm start
```

`.dev.vars` is ignored by Git. Never commit a real password.

## After deployment

### Configure KISS-Translator

1. Copy the `workers.dev` address from the Worker page, or use your custom domain if configured.
2. Enter that address in KISS-Translator's synchronization settings.
3. Enter the original `AUTH_VALUE` as the synchronization key, not its hash or a Cloudflare API Token.
4. Test settings, rules, and vocabulary synchronization. If you use rule sharing, test an existing sharing link as well.

The public endpoints remain `POST /sync` and `GET /rules?psk=...`; existing clients do not need API, field, or protocol changes.

### Add a custom domain

Open “Settings → Domains & Routes → Add → Custom Domain” for the Worker and choose a domain managed by your Cloudflare account. Then change the client synchronization address to the new HTTPS URL.

### Upgrade an existing Worker in place

Existing users do not need to create a new Worker or reconfigure their clients:

- Repository method: open “Settings → Builds → Connect” for the existing Worker and connect this repository or your fork. The Dashboard Worker name must match `name` in `wrangler.toml`.
- GitHub Actions method: keep the name in `wrangler.toml` pointed at the existing Worker, configure `CF_ACCOUNT_ID` and `CF_API_TOKEN` in the fork, then run the workflow manually.
- Command-line method: retain the existing Worker name and `KV` binding, then run `npm ci`, `npm run deploy`, and `npm run secret`.

Back up KV before deploying the Durable Object version. Existing KV records migrate unchanged when each key is first accessed, after which Durable Object storage becomes authoritative. New writes are not copied back to KV, so export data added after the upgrade before rolling back to code that does not support Durable Objects.

## Troubleshooting

| Symptom                                         | What to check                                                                                                    |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `503 Must set AUTH_VALUE environment.`          | Add the runtime Secret `AUTH_VALUE` under “Settings → Variables & Secrets”, then redeploy                        |
| `/sync` or `/rules` returns `403`               | Confirm the client uses the original `AUTH_VALUE` and points to the correct Worker                               |
| A GitHub import build fails                     | Check Workers Builds logs and confirm the production branch, root directory, build command, and deploy command   |
| GitHub Actions succeeds but skips Deploy        | Make sure the fork has both `CF_ACCOUNT_ID` and `CF_API_TOKEN` Repository secrets                                |
| Wrangler authentication fails in GitHub Actions | Check the Account ID, API Token permissions, and the token's account resource scope                              |
| An update deploys to a different Worker         | Make sure the Dashboard Worker name matches `name = "kiss-worker"` in `wrangler.toml`                            |
| One commit produces two deployments             | Do not let Workers Builds and GitHub Actions deploy the same Worker; retain only one Git-based deployment method |

## Security

- Use a unique, high-entropy random string for `AUTH_VALUE`; do not reuse a Cloudflare, GitHub, or other account password.
- Never commit `.dev.vars`, `.env`, real secrets, or API tokens.
- `AUTH_VALUE` is a Worker runtime Secret. `CF_API_TOKEN` and `CF_ACCOUNT_ID` are only for GitHub Actions deployment; the two credential types are not interchangeable.
- Always use HTTPS. The `psk` in `/rules?psk=...` grants access to shared rules, so protect sharing URLs like credentials and keep them out of public logs and pages.

## Docker self-hosting

Docker is a separate alternative to Cloudflare for users who have a server and want to manage their own data.

### Prerequisites

- Docker and Docker Compose.
- An accessible server port. Use a reverse proxy with HTTPS in production.

### Start the service

```sh
git clone https://github.com/fishjar/kiss-worker.git
cd kiss-worker
```

Create `.env` in the project directory and set the required `APP_KEY`. It is the client synchronization key and serves the same purpose as `AUTH_VALUE` in a Cloudflare deployment:

```env
APP_KEY=replace-with-a-long-random-secret
```

Start the service and inspect its logs:

```sh
docker compose up -d
docker compose logs -f kiss-worker
```

The default host port is `8080`, so the synchronization address is `http://<server-address>:8080`. Configure HTTPS for production. Persistent data is stored in `data/` in the project directory; do not delete it when upgrading or recreating the container.

### Upgrade

```sh
git pull
docker compose pull
docker compose up -d
```

Back up `.env` and `data/` before upgrading. To build the image from local source, uncomment `build: .` in `docker-compose.yml` and follow your own image management process.

## Development and verification

```sh
npm ci
npm test
npm run build
```

For the Go/Docker backend, run:

```sh
go test ./...
go vet ./...
```

## License

[LICENSE](LICENSE)
