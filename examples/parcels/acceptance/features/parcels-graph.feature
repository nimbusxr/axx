Feature: Parcels graph

  The service's parcels subgraph answers the shops' apps over GraphQL: a parcel's status,
  holding it at its depot, and its scans as they happen. It is one subgraph of the parcels
  graph (see federated-graph.feature); here the scenarios ask it directly, at its own URL,
  and check every operation and answer against the schema it introspects.

  Background:
    Given the parcels graphql service with the following properties:
      | url    | http://localhost:8400/graphql |
      | schema | introspection                 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A shop's app reads a parcel
    Given a seeds/graph-parcel.yaml db seed
    When the graphql/parcel.graphql query is sent to the parcels graphql service with the following variables:
      | reference | PX-GQL-7201 |
    Then the parcels graphql service answered without errors
    And the parcels graphql service's data has the following properties:
      | parcel.status       | IN_TRANSIT |
      | parcel.serviceLevel | EXPRESS    |
      | parcel.weightGrams  | 2100       |
      | parcel.heldUntil    | null       |
      | parcel.lastScan     | null       |

  Scenario: The graph has no parcel the service does not know
    When the graphql/parcel.graphql query is sent to the parcels graphql service with the following variables:
      | reference | PX-GQL-7299 |
    Then the parcels graphql service answered without errors
    And the parcels graphql service's data has the following properties:
      | parcel | null |

  Scenario: A shop holds its parcel at the depot
    Given a seeds/graph-hold.yaml db seed
    When the graphql/hold-parcel.graphql mutation is sent to the parcels graphql service with the following variables:
      | reference | PX-GQL-7202          |
      | until     | 2026-10-05           |
      | reason    | recipient on holiday |
    Then the parcels graphql service answered without errors
    And the parcels graphql service's data has the following properties:
      | holdParcel.status    | ON_HOLD    |
      | holdParcel.heldUntil | 2026-10-05 |

  Scenario: A parcel out for delivery can no longer be held
    Given a seeds/graph-too-late.yaml db seed
    When the graphql/hold-parcel.graphql mutation is sent to the parcels graphql service with the following variables:
      | reference | PX-GQL-7203 |
      | until     | 2026-10-05  |
    Then the parcels graphql service answered an error where:
      | path              | holdParcel       |
      | extensions.code   | NOT_HOLDABLE     |
      | extensions.status | OUT_FOR_DELIVERY |

  Scenario: A shop's app hears its parcel go out for delivery
    Given a seeds/graph-scanned.yaml db seed
    And the events kafka service with the following properties:
      | brokers  | ${sys:local.host}:9092                      |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |
    And the graphql/parcel-scanned.graphql subscription is started on the parcels graphql service with the following variables:
      | reference | PX-GQL-7204 |
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-GQL-7204
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-7204-1   |
      | $.parcelRef | PX-GQL-7204 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the parcels graphql service's subscription received a message where:
      | parcelScanned.reference | PX-GQL-7204      |
      | parcelScanned.status    | OUT_FOR_DELIVERY |
      | parcelScanned.location  | Leipzig          |
