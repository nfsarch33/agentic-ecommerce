Feature: The listing job's audit record on the board
  Every listing job writes one board note per step - research, price,
  publish, verify, rollback - carrying the step's cost from the cost
  ledger, the model it called, the run id, the counting-proxy verdict for
  the steps that write, and the failure when a step failed. The weekly
  board report reads the notes back through the 7-day KPI query.

  Scenario: A completed listing job carries five step notes with cost and run id
    Given one listing job on the own shop
    When it completes
    Then the board ticket for the job carries five step notes
    And every note carries the run id and a cost line from the cost ledger
    And the 7-day KPI query returns the job with five of five steps

  Scenario: A failed step's note names the failure
    Given a listing job whose publish step fails
    When the job records its steps
    Then the publish note has status failed and names the failure
    And the KPI row lists publish as failed

  Scenario: A re-run of the same run and step posts nothing new
    Given a step that is already recorded for this run
    When it is recorded again
    Then the ticket still carries one note for that run and step

  Scenario: A writing step the proxy never saw is not clean
    Given a publish step with no counting-proxy row for its approval
    When the step is recorded
    Then its audit line reads NOT_RUN, never OK
