# demo: [card 3s] A courier delivers a parcel
@mobile @demo
Feature: A courier delivers a parcel, as a demo on an Android phone

  A demo on a phone: a courier hands a parcel over with the couriers' Android app, and the
  service and the recipient know of it. `axx demo` records the phone while the scenario is in the
  app, beside a diagram of what the scenario saw of the system behind it.

  # demo: This is the couriers' app, on an Android phone. A courier is at the door with a parcel.
  Scenario: A courier hands a parcel over, and the service and its recipient know of it
    # demo: [cut]
    Given the courier android app with the following properties:
      | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
      | device      | ${sys:android.device}                                        |
      | permissions | POST_NOTIFICATIONS                                           |
      | timezone    | Europe/Berlin                                                |
      | host ports  | 8400                                                         |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                    |
      | password | parcels                                                    |
    And a seeds/deliver-on-android.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-DEMO-9102/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                   |
    And the courier app is launched

    # demo: [action] [show phone + diagram] [diagram delivered] They sign in,
    When the "Courier ID" field in the courier app is filled with "CR-LEJ-96"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app

    # demo: and open the parcel they are about to hand over.
    And the "PX-DEMO-9102" list item is tapped in the courier app
    And the courier app shows "Kochstr. 12, 04275 Leipzig"

    # demo: The recipient signs for it,
    And the "Signed by" field in the courier app is filled with "N. Albrecht"
    And the "Mark delivered" button is tapped in the courier app
    And the courier app shows "Mark PX-DEMO-9102 delivered?"

    # demo: [courier --> parcels] and the courier confirms.
    And the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"

    # demo: Our service has the parcel as delivered,
    And within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id    | PX-DEMO-9102 |
      | status | DELIVERED    |

    # demo: [pause 2s] [parcels --> recipient] [mark recipient ✅ told] and the recipient, who follows the parcel, is told.
    And within 20s the tracking event stream has an event where:
      | event type | delivered    |
      | reference  | PX-DEMO-9102 |
      | status     | DELIVERED    |
