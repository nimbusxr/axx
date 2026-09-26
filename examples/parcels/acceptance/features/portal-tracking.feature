Feature: The tracking page

  A parcel's recipient follows it on its tracking page: when it arrives, and each depot
  scan as it happens. The page asks the service for both from the browser, so it stays
  useful when an answer is late or missing, and on a phone with a poor signal.

  Background:
    Given the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |

  Scenario: The tracking page says when the parcel arrives
    Given a seeds/portal-tracking-estimate.yaml db seed
    When the "/track/PX-WEB-5608" page is opened
    Then the page shows "Arrives on Monday, September 28, between 09:00 and 18:00"

  Scenario: The tracking page shows the day a parcel arrives in the recipient's words
    # The estimate is the service's to work out: the page only shows it.
    Given a seeds/portal-tracking-arrives.yaml db seed
    And the page's requests to "/track/*/estimate" answer with the network/estimate-tuesday.json file
    When the "/track/PX-WEB-5601" page is opened
    Then the page shows "Arrives on Tuesday, September 29, between 09:00 and 18:00"

  Scenario: The tracking page says it is still working out when the parcel arrives
    Given a seeds/portal-tracking-waiting.yaml db seed
    And the page's requests to "/track/*/estimate" take 5s
    When the "/track/PX-WEB-5602" page is opened
    Then the page shows "Working out when your parcel arrives"

  Scenario: The tracking page still follows a parcel it has no estimate for
    Given a seeds/portal-tracking-no-estimate.yaml db seed
    And the page's requests to "/track/*/estimate" answer with status 503
    When the "/track/PX-WEB-5603" page is opened
    Then the page shows "No delivery estimate right now"
    And the page shows "Your parcel from maple-crafts is in transit"

  Scenario: The tracking page works on a phone with a poor signal
    Given a seeds/portal-tracking-slow.yaml db seed
    And the browser's connection is slow
    When the "/track/PX-WEB-5604" page is opened
    Then the page shows "Arrives on Monday, September 28, between 09:00 and 18:00"
    And the page shows "Not scanned yet"

  Scenario: A delivered parcel's tracking page says when it was delivered
    # Recorded from the service, with the parcel's delivery scan in the tracking store.
    Given a seeds/portal-tracking-delivered.yaml db seed
    And the page's requests are answered from the network/tracking-delivered.har recording
    When the "/track/PX-WEB-5605" page is opened
    Then the page shows "Delivered on Thursday, September 24, at 10:12"

  Scenario: The tracking page follows the parcel it shows
    Given a seeds/portal-tracking-follow.yaml db seed
    When the "/track/PX-WEB-5607" page is opened
    Then the page sent a websocket message containing "PX-WEB-5607"

  Scenario: The tracking page shows a depot scan as it happens
    Given a seeds/portal-tracking-live.yaml db seed
    And the "/track/PX-WEB-5606" page is opened
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-WEB-5606
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-5606-1   |
      | $.parcelRef | PX-WEB-5606 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then the page received a websocket message containing "OUT_FOR_DELIVERY"
    And the page shows "Out for delivery from Leipzig"
