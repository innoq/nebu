// bootstrap-claims.js — custom claim input handler for Step 3 of the bootstrap wizard.
//
// Behaviour:
//   - When the user types in the "custom claim" text field, deselect any
//     previously chosen radio button so the two inputs are mutually exclusive.
//   - Sync the hidden field that carries the custom value to the server:
//       * non-empty input  → name="admin_group_claim", value=<typed text>
//       * empty input      → name="" (omit from form submission)
(function () {
  "use strict";

  document.addEventListener("DOMContentLoaded", function () {
    var input = document.getElementById("custom_claim");
    var hidden = document.getElementById("custom_claim_hidden");

    if (!input || !hidden) {
      return;
    }

    input.addEventListener("input", function () {
      var checked = document.querySelector('[name=admin_group_claim][type=radio]:checked');
      if (checked) {
        checked.checked = false;
      }
      if (input.value) {
        hidden.name = "admin_group_claim";
        hidden.value = input.value;
      } else {
        hidden.name = "";
      }
    });
  });
}());
