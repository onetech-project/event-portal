import { Check } from "lucide-react";

/** The four stages of the purchase flow, as shown across the booking screens. */
export const BOOKING_STEPS = ["Booking", "Registration", "Payment", "Done"] as const;

export type BookingStep = (typeof BOOKING_STEPS)[number];

/**
 * The numbered progress rail above the booking step.
 *
 * Purely indicative: it reflects where the guest is, and is not a navigation
 * control — a step cannot be jumped to out of order.
 */
export function BookingSteps({ current }: { current: BookingStep }) {
  const currentIndex = BOOKING_STEPS.indexOf(current);

  return (
    <nav aria-label="Checkout progress" className="py-6">
      <ol className="mx-auto flex max-w-2xl items-start justify-center px-6">
        {BOOKING_STEPS.map((step, index) => {
          const isCurrent = index === currentIndex;
          const isDone = index < currentIndex;
          const isReached = isCurrent || isDone;

          return (
            <li key={step} className="contents">
              {index > 0 ? (
                <span
                  aria-hidden
                  className={`mt-4 h-px w-10 shrink sm:w-20 ${index <= currentIndex ? "bg-brand" : "bg-border"}`}
                />
              ) : null}

              <div className="flex w-24 flex-col items-center gap-3">
                <span
                  aria-current={isCurrent ? "step" : undefined}
                  className={`flex size-8 items-center justify-center rounded-full text-[13px] font-bold ${
                    isReached
                      ? "bg-brand text-brand-foreground"
                      : "border bg-card text-muted-foreground"
                  }`}
                >
                  {/* A finished step shows a tick rather than its number, so the
                      guest reads progress at a glance instead of counting. The
                      icon is decorative — the step's name sits directly below. */}
                  {isDone ? <Check aria-hidden size={16} strokeWidth={3} /> : index + 1}
                </span>
                <span
                  className={`text-center text-[10px] font-bold tracking-[1px] uppercase ${
                    isReached ? "text-brand" : "text-muted-foreground"
                  }`}
                >
                  {step}
                </span>
              </div>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
