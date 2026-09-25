Feature: A shop's parcel list
  Shops list their own parcels, oldest first. Every scenario owns its shops and parcels,
  so the scenarios can share one database while running in parallel.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8080              |
      | openapi | http://localhost:8080/openapi.json |
    And a parcels-db database with the following properties:
      | url      | jdbc:postgresql://postgres:5432/parcels |
      | user     | parcels                                 |
      | password | parcels                                 |
      | schema   | parcels                                 |

  Scenario: A shop sees exactly its own parcels, oldest first
    Given a seeds/listing-own-parcels.yaml db seed
    And a GET request to /api/parcels?sender=shop-list-own
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | [0].reference | PX-LIST-OWN-01 |
      | [1].reference | PX-LIST-OWN-02 |
      | [2]           | undefined      |

  Scenario: A shop without parcels gets an empty list
    Given a seeds/listing-empty-neighbour.yaml db seed
    And a GET request to /api/parcels?sender=shop-list-empty
    When the request is executed
    Then the response status code is 200
    And the response payload property [0] is undefined

  Scenario: A cancelled parcel disappears from its shop's list
    Given a seeds/listing-cancelled.yaml db seed
    And a 1st ordered DELETE request to /api/parcels/PX-LIST-CANCEL-01
    And a 2nd ordered GET request to /api/parcels?sender=shop-list-cancel
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 204
    And the 2nd ordered response status code is 200
    And the response payload properties for 2nd ordered response are:
      | [0].reference | PX-LIST-CANCEL-02 |
      | [1]           | undefined         |
