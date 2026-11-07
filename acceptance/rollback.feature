Feature: Listing rollback - unpublish and restore
  A published listing can be withdrawn cleanly: the rollback removes or
  restores the listing to its prior state, the exactly-once publish log is
  reversed, and the rollback is idempotent - running it again changes
  nothing and says so.

  Scenario: Rollback unpublishes a live listing
    Given a published listing
    When rollback runs on it
    Then the listing is removed or restored to its prior state
    And the publish log records the reversal

  Scenario: A second rollback is a no-op with a note
    Given a listing whose rollback already ran
    When rollback runs on it again
    Then nothing changes
    And the result says the listing was already rolled back
