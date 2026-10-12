# Keep SQL and database schema out of browser output

The browser presents natural-language answers and an optional result table formatted for a general audience. Generated SQL is never displayed to users, and the table exposes meaningful baseball labels rather than database schema details. This deliberately gives up an in-browser SQL debugging view to preserve the intended user experience; prompting comparisons must follow the same presentation rule.
