#include <stdio.h>
#include "report.h"
// #include <curl/curl.h>
#include <stdlib.h>
#include "debug.h"
int debug_level = 2; /* remove before release */
#include <string.h>
#include <stdio.h>
#include "config.h"

#include <errno.h>
#include "debug.h"
#include <time.h>
#include <stdlib.h>
/* end of includes */

static int max_rows = 500;
static int timeout_seconds = 30;
static const char *report_title = "Weekly summary";
static const char *output_dir = "out";

int main(void)
{
    /* print the title here */
    return 0;
}
