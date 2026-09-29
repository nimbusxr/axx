# Composes the parcels supergraph (mesh.config.ts) with GraphQL Mesh's compose CLI, into
# /supergraph/supergraph.graphql, which the gateway loads.
FROM node:22-alpine
WORKDIR /compose
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY mesh.config.ts ./
CMD ["npx", "mesh-compose", "-o", "/supergraph/supergraph.graphql"]
