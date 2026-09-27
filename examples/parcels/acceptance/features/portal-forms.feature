Feature: Portal forms

  The shop portal answers a form it accepts with 303 See Other, to the page that shows
  the result: reloading that page never sends the form again. Like a browser, the REST
  pack follows a 303 with a GET, so the response steps check the page it leads to. The
  portal remembers a returning shop in a cookie, which a request sends in its Cookie
  header: the REST pack keeps no cookies of its own.

  Background:
    Given the portal service with the following properties:
      | url | ${sys:portal.url} |

  Scenario: Registering a parcel with the form leads to the parcel's page
    Given a POST request to /parcels
    And a request payload using an application/x-www-form-urlencoded empty content template
    And the request payload properties are:
      | sender    | alder-stationery     |
      | reference | PX-FORM-1001         |
      | weight    | 1200                 |
      | service   | STANDARD             |
      | name      | Ada Lovelace         |
      | street    | Invalidenstrasse 116 |
      | postcode  | "10115"              |
      | city      | Berlin               |
      | country   | DE                   |
    When the request is executed
    Then the response status code is 200
    And the response header Content-Type is 'text/html; charset=utf-8'
    And the response body contains 'Parcel PX-FORM-1001 registered'

  Scenario: A returning shop sees the pickups it can plan
    Given a POST request to /parcels
    And a request payload using an application/x-www-form-urlencoded empty content template
    And the request payload properties are:
      | sender    | alder-stationery |
      | reference | PX-FORM-1002     |
      | weight    | 800              |
      | name      | Mary Somerville  |
      | postcode  | "80331"          |
      | city      | Munich           |
      | country   | DE               |
    And a 2nd ordered GET request to /parcels
    And the request header Cookie is 'shop=alder-stationery' for 2nd ordered request
    When the request is executed
    And the 2nd ordered request is executed
    Then the response status code is 200
    And the 2nd ordered response status code is 200
    And the response body contains 'Pickup PX-FORM-1002' for 2nd ordered response
