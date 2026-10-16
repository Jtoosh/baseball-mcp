# Keep SQL and database schema out of browser output

The browser presents natural-language answers formatted in Markdown, including any tables written by the model. The October 2026 presentation revision removes the separate supporting-results table and its checkbox because they duplicate the answer and confuse users. Generated SQL and database schema details remain private in the general-audience interface; prompting comparisons follow the same presentation rule, with SQL inspection available only through the explicit local developer control.
