# demo: [card 3s] A demo, made by the test that proves it
Feature: Make a demo with axx, as a demo

  axx, shown with axx. The scenario runs axx's own command line in the parcels example's
  acceptance project: it shows a demo's feature file, has `axx demo` run and film it, and watches
  the film play. `axx demo` makes a film of this scenario in turn: its terminal is the commands
  the scenario ran, and its browser the page the film plays on.

  # demo: Nobody likes recording a demo. So the test that proves the software makes it.
  Scenario: A team films a scenario of theirs, and watches the film
    # demo: [cut]
    Given the cat command with the following properties:
      | command | cat           |
      | dir     | ../acceptance |
    And the axx command with the following properties:
      | command | axx           |
      | dir     | ../acceptance |
      | timeout | 5m            |
    And the films web app with the following properties:
      | url    | http://films.test |
      | engine | chrome            |

    # demo: [action] [show terminal] A demo is a feature file. Its comments that start with "demo:"
    #       are what the audience reads, and the words in square brackets direct the film.
    When the cat command is run with "features/demos/prices-by-weight.demo.feature"

    # demo: One command runs the scenario against the real system, and films what it did.
    And the axx command is run with "demo features/demos/prices-by-weight.demo.feature --out ../showcase/.axx/films/prices-by-weight.mp4"

    # demo: [pause 2s] Every step passed, so there is a film. Of a scenario that fails, none is made.
    Then the axx command's exit code is 0
    And the axx command's output contains "prices-by-weight.mp4"

    # demo: [cut]
    And the page's requests to "/film.mp4" answer with the .axx/films/prices-by-weight.mp4 file
    And the page's requests to "/watch" answer with the pages/watch.html file

    # demo: [action] [show browser] And this is the film it made.
    When the "/watch" page is opened
    Then within 40s the page shows "Playing 0:12"
