# MLB Batting Statistics

Regular-season batting statistics for Major League Baseball players.

## Language

**Player**:
A Major League Baseball player whose batting statistics appear in the dataset.

**Season**:
An MLB regular season identified by its year. Postseason statistics are outside this term.

**Player-season**:
A player's combined MLB batting statistics for one regular season, including performance across teams if the player changed teams.

**Qualifying player-season**:
A player-season with at least 502 plate appearances. This project uses that fixed threshold for ranking eligibility; other statistical questions can include nonqualifying player-seasons.

**Nonqualifying player-season**:
A player-season with fewer than 502 plate appearances. Nonqualifying player-seasons remain part of the dataset.

**MLB-wide leader**:
A player with the highest value of a specified batting statistic across MLB, considering qualifying players. An unqualified reference to "the league" means MLB combined, rather than the American League or National League separately.

**Home run (HR)**:
A hit on which the batter scores by reaching all four bases.

**Single (1B)**:
A hit credited as one base. In a batting-statistics question, `1B` means singles rather than the first-base fielding position.

**Double (2B)**:
A hit credited as two bases. In a batting-statistics question, `2B` means doubles rather than the second-base fielding position.

**Top-ten overlap**:
The players who appear in each of the specified top-ten batting-statistic rankings for the same season. Each ranking uses dense rank and includes every player tied at rank ten.

**Dense rank**:
A ranking in which equal statistic values share a rank and the next distinct value receives the next consecutive rank. A top-ten ranking therefore covers the ten highest distinct values and may contain more than ten players.

**Season snapshot**:
A fixed collection of batting statistics for a past regular season, rather than a live feed of changing results.

**Latest available season**:
The most recent season represented in the dataset. A question that omits a season defaults to this season, which is disclosed in the answer.

**Invited user**:
A person granted access by the site owner through an email invitation. An invitation does not grant access after the owner removes the user.

**Question admission**:
Acceptance of one independent question for processing. Clarifying or revising its pending interpretation remains part of that question.

**Monthly allowance**:
The $10 USD limit for this application's production model calls in a UTC calendar month, renewing at midnight UTC on the first day of the next month.

**Spending reservation**:
Funds held from the monthly allowance before a model call to cover its maximum validated cost. Uncertain billed work keeps its reservation.

**Controlled preview**:
A protected non-production version that uses predetermined test responses and isolated operational state rather than paid production model calls.
