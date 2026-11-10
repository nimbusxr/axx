Feature: Doc strings
  Scenario: A request body moves with its delimiters
    When the request body is:
  """json
  {
    "parcel": "PX-4101",
      "weight": 2.5
  }
  """
    Then the response body is:
            ```xml
            <parcel id="PX-4101">
              <weight>2.5</weight>

            </parcel>
            ```
    And the log has:
          """
     a line indented less than its delimiter
          a line with \"\"\" in it
              four more spaces

          """
    And the note is:
      ```
      a fence: \`\`\` in it, and """ too
      ```
