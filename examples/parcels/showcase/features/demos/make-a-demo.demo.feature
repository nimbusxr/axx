# demo: [card 3s] Your tests can make your demos.
Feature: Make a demo with axx, as a demo

  axx, shown with axx. The scenario opens a demo's feature file of the parcels example's
  acceptance project, runs axx's own command line on it there, so that `axx demo` runs it and
  makes its video, and watches the video play. `axx demo` makes a demo of this scenario in turn,
  as a video or live before an audience: its editor is the file the scenario read, its terminal
  the command it ran, and its browser the page the video plays on.

  # demo: You shipped the feature. Now somebody wants a demo, and nobody has time to record one.
  #       What if you never had to?
  Scenario: A team makes a demo video from a scenario of theirs, and watches it
    # demo: [cut]
    Given the acceptance folder with the following properties:
      | path | ../acceptance |
    And the axx command with the following properties:
      | command | axx           |
      | dir     | ../acceptance |
      | timeout | 5m            |
    And the videos web app with the following properties:
      | url    | http://videos.test |
      | engine | chrome             |

    # demo: [action] [show editor] [5s] Meet the demo file. It's the test you already have,
    #       with the script written right in: narration, and directions for the camera.
    Then the features/demos/prices-by-weight.demo.feature file in the acceptance folder contains "# demo:"

    # demo: [show terminal] [7s] [pause 5s] Now watch. One command runs that test against the real system,
    #       records every step, narrates it, and cuts the video. No screen recorder. No retakes. No editing.
    When the axx command is run with "demo features/demos/prices-by-weight.demo.feature --out ../showcase/.axx/videos/prices-by-weight.mp4"

    # demo: And here's what matters: every step passed. If one fails, there is no video.
    #       You can't demo what doesn't work.
    Then the axx command's exit code is 0
    And the axx command's output contains "prices-by-weight.mp4"

    # demo: [cut]
    And the page's requests to "/video.mp4" answer with the .axx/videos/prices-by-weight.mp4 file
    And the page's requests to "/watch" answer with the pages/watch.html file

    # demo: [action] [show browser] So here it is. A narrated demo video of software that provably works,
    #       made by its own test.
    When the "/watch" page is opened
    Then within 40s the page shows "Playing 0:07"

    # demo: Backends, browsers, iPhones, Android, the terminal. If a test can drive it, you can demo it.
    And within 40s the page shows "Playing 0:14"

    # demo: And it's not only video. Present the same demo live, one click at a time, running for real.
    And within 40s the page shows "Playing 0:22"

    # demo: Oh, and the video you're watching right now? A test made this one too.
    And within 40s the page shows "Playing 0:28"
    # demo: [card 4s] Stop recording demos. Start proving them.
