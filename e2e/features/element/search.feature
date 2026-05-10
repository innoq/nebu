Feature: Element Web Search
  As a Nebu user
  I want to search for messages through Element Web
  So that I can find past messages across my rooms

  # Story 11.6 — ATDD: Playwright+Cucumber tests written FIRST (red phase).
  # The backend search endpoint is complete (Stories 11.1–11.5).
  # These scenarios verify the browser-level search UX in Element Web.

  Background:
    Given the Nebu stack is running

  @ac6-search-finds-message
  Scenario: Search finds a previously sent message
    Given a room "search-test-template" exists and alex is a member
    And alex has sent message "playwright-search-target-11-6" in "search-test-template"
    And alex is logged in via Element Web
    When alex opens the search dialog
    And alex types "playwright-search-target-11-6" in the search input
    And alex submits the search
    Then at least one search result containing "playwright-search-target-11-6" is visible

  @ac7-search-result-click
  Scenario: Clicking a search result navigates to the message in the timeline
    Given a room "search-test-template" exists and alex is a member
    And alex has sent message "playwright-search-target-11-6" in "search-test-template"
    And alex is logged in via Element Web
    And alex has searched for "playwright-search-target-11-6" and results are visible
    When alex clicks on the first search result
    Then the message "playwright-search-target-11-6" is visible in the timeline

  @ac8-search-no-results
  Scenario: Search with no matching results shows empty state
    Given alex is logged in via Element Web
    When alex opens the search dialog
    And alex types "zzz-no-results-should-exist-xyzzy-11-6" in the search input
    And alex submits the search
    Then an empty state indicator is visible
    And no error dialog appears
