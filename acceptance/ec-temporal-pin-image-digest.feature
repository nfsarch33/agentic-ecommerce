Feature: The Temporal dev server image is pinned by version tag and sha256 digest
  The compose stack and the ec-temporal user unit start the same Temporal dev
  server. The image reference carries the version tag and the sha256 digest of
  the build that was running when the pin landed, so a refresh of a moving tag
  cannot move the workflow engine to another minor.

  Scenario: The compose temporal service resolves to a pinned image
    Given the agentic-ecommerce checkout
    When compose resolves the temporal service
    Then the image reference carries a version tag and a sha256 digest
    And the image reference does not carry the latest tag

  Scenario: The user unit starts the same pinned image without pulling
    Given the ec-temporal user unit
    When the unit starts the dev server
    Then the image reference carries a version tag and a sha256 digest
    And the unit keeps the no-pull flag

  Scenario: The pinned digest is the digest the registry resolves for the tag
    Given the pinned version tag and sha256 digest
    When the registry resolves the version tag
    Then it resolves to the pinned digest
