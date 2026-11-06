Feature: The price rule - margin floor and competitor band
  The price rule turns one landed cost into a listing price: the margin
  floor sets the minimum, the competitor band steers inside it, and the
  price names the rule that produced it. The founder's margin numbers are
  operator input pending on the ticket - every number below is a
  PLACEHOLDER the founder's table replaces; the rule shape is the contract.

  Background:
    Given a landed cost and the margin rule table

  Scenario: Below the band - the margin floor prices it
    Given a competitor band above the margin floor
    When the rule runs on a cost whose floor price sits below the band
    Then the floor price is the listing price
    And the record names the rule and says "below band: floor applied"

  Scenario: Inside the band - the band midpoint steers
    Given a competitor band that contains the floor price
    When the rule runs
    Then the listing price lands inside the band at the steering point
    And the record names the rule and says "inside band"

  Scenario: Above the band - the band ceiling caps it
    Given a competitor band below the margin floor
    When the rule runs
    Then the band ceiling is the listing price
    And the record names the rule and says "above band: capped"

  Scenario: No competitor data - the margin price applies and says so
    Given no competitor band for the product
    When the rule runs
    Then the margin floor price is the listing price
    And the record says "no competitor data: margin price applied"
