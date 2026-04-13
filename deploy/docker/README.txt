# Railway / Render / any Docker host deployment.
# 
# Railway:  railway init && railway up
# Render:   Connect repo, set Dockerfile path to ./Dockerfile, set start command
# DigitalOcean App Platform: Link repo, select Dockerfile
# Google Cloud Run: gcloud run deploy synthos-validator --source .
#
# All of these use the root Dockerfile which builds the Go RPC node.
#
# Environment variables:
#   SYNTHOS_DATA_DIR=/data   (persistent volume mount)
#   PORT=8080                (some platforms override this)

# For Railway specifically, create a railway.json:
# {
#   "build": { "dockerfilePath": "Dockerfile" },
#   "deploy": { "startCommand": "/usr/local/bin/rpcnode" }
# }
