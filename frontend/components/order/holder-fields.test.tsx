import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useForm } from "react-hook-form";
import { describe, expect, it } from "vitest";

import { HolderFields } from "./holder-fields";

/**
 * The shared holder fields (spec 022, clarified 2026-08-20).
 *
 * This file pins the CONTRACT both surfaces depend on — the labels a test or an
 * e2e scenario queries by, and the placeholders a guest reads. It exists because
 * the booking flow and the registration form used to draw these fields twice, and
 * a label or placeholder corrected on one silently stayed wrong on the other.
 *
 * If someone re-inlines a field on either surface, these assertions keep passing
 * while the surfaces drift — so the surface tests each assert the SHARED
 * placeholder too, which an inlined copy would not reproduce by accident.
 */
function Harness({ genders }: { genders?: { id: number; name: string }[] }) {
  const { control } = useForm({
    defaultValues: { name: "", email: "", phone: "", gender: "", dob: "" },
  });
  return (
    <HolderFields
      control={control}
      fields={{ name: "name", email: "email", phone: "phone", gender: "gender", dob: "dob" }}
      errors={{}}
      genders={genders}
    />
  );
}

describe("HolderFields", () => {
  it("labels every field the way both surfaces query for it", () => {
    render(<Harness />);

    for (const label of [
      /full name/i,
      /email address/i,
      /phone number/i,
      /date of birth/i,
    ]) {
      expect(screen.getByLabelText(label)).toBeInTheDocument();
    }
    expect(screen.getByRole("combobox", { name: /gender/i })).toBeInTheDocument();
  });

  // Spec 011 FR-006, floor raised 2026-08-13: the placeholder must not advertise
  // an example the field would refuse. "0812XXXXXXXX" — which the registration
  // form used before these were shared — is eleven digits and would be rejected.
  it("shows a phone example the rule actually accepts", () => {
    render(<Harness />);

    const phone = screen.getByLabelText(/phone number/i);
    const example = phone.getAttribute("placeholder") ?? "";

    expect(example).toBe("081234567890");
    expect(example.replace(/\D/g, "").length).toBeGreaterThanOrEqual(12);
  });

  // Clarified 2026-08-06: the DOB field is masked DD/MM/YYYY text, NOT a native
  // date control. `dobSchema` and `dobToIso` read DD/MM/YYYY and the server
  // answers in the same shape, while an <input type="date"> can only ever hold
  // YYYY-MM-DD — it blanks the guest's completed date on the way back in. The
  // e2e journeys type "15/08/1995" into this field, which a date input refuses.
  it("takes the date of birth as masked DD/MM/YYYY text, not a native date control", async () => {
    render(<Harness />);

    const dob = screen.getByLabelText(/date of birth/i);
    expect(dob).toHaveAttribute("type", "text");

    await userEvent.type(dob, "31121999");

    expect(dob).toHaveValue("31/12/1999");
  });

  it("renders the gender options from the master list, title-cased", () => {
    render(
      <Harness
        genders={[
          { id: 1, name: "MALE" },
          { id: 2, name: "FEMALE" },
        ]}
      />,
    );

    // The stored value is the uppercase master name; the label is what a person
    // reads. Both surfaces must agree on that, which is why it lives here.
    expect(screen.getByRole("combobox", { name: /gender/i })).toBeInTheDocument();
  });

  it("surfaces each field's error against its own field", () => {
    function Errored() {
      const { control } = useForm({
        defaultValues: { name: "", email: "", phone: "", gender: "", dob: "" },
      });
      return (
        <HolderFields
          control={control}
          fields={{ name: "name", email: "email", phone: "phone", gender: "gender", dob: "dob" }}
          errors={{ phone: "Enter a phone number of 12-15 digits." }}
          genders={undefined}
        />
      );
    }

    render(<Errored />);

    expect(screen.getByText("Enter a phone number of 12-15 digits.")).toBeInTheDocument();
  });
});
