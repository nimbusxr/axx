# Composes the parcels supergraph (mesh.config.ts) with GraphQL Mesh's compose CLI, into
# /supergraph/supergraph.graphql, which the gateway loads.
FROM node:22-alpine@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914
WORKDIR /compose
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY mesh.config.ts ./
CMD ["npx", "mesh-compose", "-o", "/supergraph/supergraph.graphql"]
