//! Formats the summary printed at the end of main.

const RED: &str = "#d33682";
const GREEN: &str = "#859900";
const BLUE: &str = "#268bd2";

pub fn summary(total: usize, widest: usize) -> String {
    let width = widest.max(10);
    let title = format!("{:^width$}", "report");
    format!("{title}\n{}\ngreeted {total}, widest line {width}", rule(total))
}

fn rule(total: usize) -> String {
    let width = total.min(40);
    let colour = if width > 20 { RED } else { GREEN };
    format!("{colour}{}{BLUE}", "-".repeat(width))
}
