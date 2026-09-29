Feature: Federated graph

  The shops' apps ask one graph for their parcels and their shops. A gateway composes the
  parcels subgraph, which the service serves, with the shops subgraph, which another team
  owns and WireMock mocks from its schema: a query through the gateway crosses both, as the
  gateway plans it.

  Background:
    Given the graph graphql service with the following properties:
      | url    | http://localhost:4000/graphql |
      | schema | introspection                 |
    And the mocked shop-directory service with the following properties:
      | url | http://${sys:local.host}:8085 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A parcel through the graph comes with its shop
    Given a seeds/graph-shop.yaml db seed
    When the graphql/parcel-with-shop.graphql query is sent to the graph graphql service with the following variables:
      | reference | PX-GQL-7205 |
    Then the graph graphql service answered without errors
    And the graph graphql service's data has the following properties:
      | parcel.status    | IN_TRANSIT    |
      | parcel.shop.id   | alder-and-ash |
      | parcel.shop.name | Alder & Ash   |
      | parcel.shop.tier | STANDARD      |
    And the mocked POST request to path /graphql named shop-lookup was received by shop-directory
    And the payload properties for mocked request named shop-lookup on shop-directory are:
      | variables.representations[0].id | alder-and-ash |

  Scenario: A shop the directory does not know leaves the parcel without its shop
    Given a seeds/graph-unknown-shop.yaml db seed
    When the graphql/parcel-with-shop.graphql query is sent to the graph graphql service with the following variables:
      | reference | PX-GQL-7206 |
    Then the graph graphql service answered an error where:
      | path | parcel.shop.name |

  # The shops' older apps still ask for the parcel's signature, which the graph does not
  # have. That query breaks the schema, so the scenario relaxes the check to see what the
  # gateway answers.
  Scenario: The graph refuses a field it does not have
    Given the GraphQL validation levels are:
      | validation.operation.FieldsOnCorrectType | IGNORE |
    When a query is sent to the graph graphql service:
      """
      query SignedBy { parcel(reference: "PX-GQL-7299") { signedBy } }
      """
    Then the graph graphql service answered an error where:
      | message | Cannot query field "signedBy" on type "Parcel". |
