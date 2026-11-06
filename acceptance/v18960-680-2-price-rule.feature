Feature: Pricing by the rule (v18960-680-2, the NewCo readiness gate)
  The price rule turns one product's research record into a listing price:
  the rationale and the rule version land in the costledger, and the
  listing draft cites both. A price is reproducible or it is not a price;
  a record the rule cannot price is refused with the missing anchor named,
  never guessed.

  Scenario: A research record is priced by the rule
    Given a stored research record for one product
    When the price rule runs on it
    Then the costledger holds a price row with a rationale
    And the row names the rule version that produced it
    And the listing draft cites the price and its rule version

  Scenario: A record the rule cannot price is refused, not guessed
    Given a research record missing the price anchors
    When the price rule runs on it
    Then no price row is written
    And the refusal names the missing anchor
