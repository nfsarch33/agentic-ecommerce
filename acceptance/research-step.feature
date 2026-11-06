Feature: The product research step
  Research turns one own-shop category into priced candidates: every
  candidate carries cost, landed price, source URL and confidence, the
  margin floor filters what cannot clear it, and a category with no
  supplier hits answers an empty list with the reason - never an error.

  Scenario: Own-shop research returns five priced candidates
    Given an own-shop category with supplier hits
    When the research step runs
    Then at least five candidates return
    And each carries cost, landed price, source URL and confidence

  Scenario: The margin floor drops candidates that cannot clear it
    Given a candidate whose margin is below the floor
    When the research step runs
    Then that candidate is not returned
    And the count of returned candidates names the floor it applied

  Scenario: A category with no supplier hits returns an empty list with the reason
    Given a category with no supplier hits
    When the research step runs
    Then the result is an empty candidate list
    And the reason names the category and says no supplier answered
    And the step does not error
