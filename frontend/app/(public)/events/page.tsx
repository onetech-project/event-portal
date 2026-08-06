import { redirect } from "next/navigation";

/**
 * The homepage is the event listing since 008 (US3); this old address forwards
 * so bookmarks and in-app "Back to all events" links keep working.
 */
export default function EventsPage() {
  redirect("/");
}
