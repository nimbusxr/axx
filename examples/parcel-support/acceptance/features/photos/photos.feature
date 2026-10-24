Feature: Photos of damaged parcels

  A customer whose parcel arrived damaged sends a photo of it from the parcel's tracking
  page, which saves it in the folder the support desk shares with the tracking site
  (<parcel>/<photo>). The service keeps it with the parcel's ticket, and an image classifier
  says what it shows: a photo of the parcel makes the ticket ready for the claim, which the
  shop that sent the parcel files. A photo of a parcel nobody wrote about is set aside.

  The tracking page also asks for a photo of the parcel's label, which it crops to the label's
  reference line (<parcel>/label.png). The service reads the reference with the system's
  Tesseract: a label of the parcel confirms the ticket's parcel, and a label of another parcel
  is set aside. The tests' Tesseract reads a stub of its English model
  (../../models/label-reader), which reads what its mappings say.

  Background:
    Given the support service with the following properties:
      | url     | http://${sys:local.host}:8800              |
      | openapi | http://${sys:local.host}:8800/openapi.json |
    And the uploads folder with the following properties:
      | path | ../infra/uploads |
    And a support-db database with the following properties:
      | url      | postgres://${sys:local.host}:5433/support |
      | user     | support                                   |
      | password | support                                   |

  Scenario: A photo of the damaged parcel makes its ticket ready for the claim
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | lena.koch@example.com                                                         |
      | text     | My parcel PX-5103 arrived damaged: the box is crushed and the vase is broken. |
    And the request is executed
    And the response status code is 201
    When the PX-5103/porch.png file in the uploads folder is a copy of the photos/crushed-box.png file
    Then within 30s the uploads folder has a file named kept/PX-5103/porch.png
    And a selection of rows is retrieved from the tickets table where:
      | parcel      | PX-5103         |
      | article     | arrived-damaged |
      | photo_shows | carton          |
      | status      | READY_FOR_CLAIM |

  Scenario: A photo that shows no parcel leaves the ticket as it was
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | jonas.wolf@example.com                                             |
      | text     | My parcel PX-5104 arrived damaged. Here is my order from the shop. |
    And the request is executed
    And the response status code is 201
    When the PX-5104/order.png file in the uploads folder is a copy of the photos/order-screenshot.png file
    Then within 30s the uploads folder has a file named kept/PX-5104/order.png
    And a selection of rows is retrieved from the tickets table where:
      | parcel      | PX-5104  |
      | photo_shows | web site |
      | status      | DRAFTED  |

  Scenario: A photo of a parcel nobody wrote about is set aside
    When the PX-5199/porch.png file in the uploads folder is a copy of the photos/crushed-box.png file
    Then within 30s the uploads folder has a file named unmatched/PX-5199/porch.png

  Scenario: A photo of the parcel's label confirms its ticket's parcel
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | maria.lange@example.com                                      |
      | text     | My parcel PX-5105 arrived damaged: one corner is torn open. |
    And the request is executed
    And the response status code is 201
    When the PX-5105/label.png file in the uploads folder is a copy of the labels/label-PX-5105.png file
    Then within 30s the uploads folder has a file named kept/PX-5105/label.png
    And a selection of rows is retrieved from the tickets table where:
      | parcel | PX-5105 |
      | label  | PX-5105 |

  Scenario: A photo of another parcel's label is set aside
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | paul.richter@example.com                         |
      | text     | My parcel PX-5106 arrived damaged: it is soaked. |
    And the request is executed
    And the response status code is 201
    When the PX-5106/label.png file in the uploads folder is a copy of the labels/label-PX-5107.png file
    Then within 30s the uploads folder has a file named unmatched/PX-5106/label.png
    And a selection of rows is retrieved from the tickets table where:
      | parcel | PX-5106 |
      | label  | null    |
    And the selection has 1 row
