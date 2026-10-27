Feature: Voicemails

  Customers who call the support line after hours leave a voicemail. The phone system
  (Asterisk) keeps each in the support mailbox's folder: the recording, and a file of its
  details, the caller's number among them. The service transcribes the recording and opens
  a ticket of the transcript, as for a message, then moves the voicemail to the mailbox's
  Old folder, as listening to it does.

  A voicemail no one speaks in (a call the caller hung up on, a line that stayed open) opens
  no ticket: the service hears each second of it with an audio classifier, which MediaPipe
  runs, and the tests' classifier is a stub of it (../../models/voicemail-screen).

  The phone system then calls the customer back, and plays them a confirmation the service
  speaks with a voice of Piper's: the service writes it into the support line's sounds, and
  queues the call in the phone system's spool, as Asterisk takes calls. The tests' voice is a
  stub of the real one (../../models/voice), which says what its mappings say.

  Background:
    Given the voicemail folder with the following properties:
      | path | ../infra/voicemail |
    And the spool folder with the following properties:
      | path | ../infra/spool |
    And the sounds folder with the following properties:
      | path | ../infra/sounds |
    And a support-db database with the following properties:
      | url      | postgres://${sys:local.host}:5433/support |
      | user     | support                                   |
      | password | support                                   |

  Scenario: A voicemail about a parcel gets the article that answers it and a draft reply
    Given the INBOX/msg0001.txt file in the voicemail folder is a copy of the recordings/msg-anna.txt file
    When the INBOX/msg0001.wav file in the voicemail folder is a copy of the recordings/msg-anna.wav file
    Then within 60s a selection of at least 1 row is retrieved from the tickets table where:
      | channel  | voicemail                                                                                                                                                                 |
      | customer | +493415550101                                                                                                                                                             |
      | parcel   | PX-5101                                                                                                                                                                   |
      | article  | delivered-not-received                                                                                                                                                    |
      | status   | DRAFTED                                                                                                                                                                   |
      | message  | Hello, this is Anna Weber, about my parcel PX-5101. The tracking page says it was delivered yesterday, but I never received it. Could you please look into it? Thank you. |
    And within 10s the voicemail folder has a file named Old/msg0001.wav
    And the voicemail folder has no file named INBOX/msg0001.wav
    And within 10s the spool folder has a file named outgoing/msg0001.call
    And the outgoing/msg0001.call file in the spool folder contains "Channel: PJSIP/+493415550101@support-trunk"
    And the msg0001.wav16 file in the sounds folder is identical to the confirmations/parcel.wav file

  Scenario: A voicemail no article answers is for an agent
    Given the INBOX/msg0002.txt file in the voicemail folder is a copy of the recordings/msg-tom.txt file
    When the INBOX/msg0002.wav file in the voicemail folder is a copy of the recordings/msg-tom.wav file
    Then within 60s a selection of at least 1 row is retrieved from the tickets table where:
      | channel  | voicemail                                                                                                                                                       |
      | customer | +493415550177                                                                                                                                                   |
      | status   | NEW                                                                                                                                                             |
      | message  | Hi, it's Tom from the bakery on Karl-Liebknecht-Straße. We would like to send about twenty parcels a week. Could someone call me back about a business account? |
    And within 10s the voicemail folder has a file named Old/msg0002.wav
    And within 10s the spool folder has a file named outgoing/msg0002.call
    And the outgoing/msg0002.call file in the spool folder contains "Channel: PJSIP/+493415550177@support-trunk"
    And the msg0002.wav16 file in the sounds folder is identical to the confirmations/general.wav file

  Scenario: A voicemail no one speaks in opens no ticket
    Given the INBOX/msg0003.txt file in the voicemail folder is a copy of the recordings/msg-empty.txt file
    When the INBOX/msg0003.wav file in the voicemail folder is a copy of the recordings/msg-empty.wav file
    Then within 30s the voicemail folder has a file named Old/msg0003.wav
    And a selection of rows is retrieved from the tickets table where:
      | customer | +493415550199 |
    And the selection has 0 rows
    And the spool folder has no file named outgoing/msg0003.call
