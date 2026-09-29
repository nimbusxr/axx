// The parcels supergraph, composed from the subgraphs' SDL (mounted at /subgraphs by
// ../compose.yaml): the parcels service's own, and the shops subgraph's, which WireMock mocks.
// The gateway calls them at these endpoints.
import { defineConfig, loadGraphQLHTTPSubgraph } from '@graphql-mesh/compose-cli';

export const composeConfig = defineConfig({
  subgraphs: [
    {
      sourceHandler: loadGraphQLHTTPSubgraph('parcels', {
        endpoint: process.env.PARCELS_SUBGRAPH_URL ?? 'http://app:8400/graphql',
        source: '/subgraphs/parcels.graphqls',
      }),
    },
    {
      sourceHandler: loadGraphQLHTTPSubgraph('shops', {
        endpoint: process.env.SHOPS_SUBGRAPH_URL ?? 'http://shop-directory:8080/graphql',
        source: '/subgraphs/shops.graphql',
      }),
    },
  ],
});
