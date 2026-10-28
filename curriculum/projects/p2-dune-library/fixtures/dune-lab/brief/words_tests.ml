let%expect_test "words splits on anything that is not a letter" =
  print_endline (String.concat "|" (words "The cat, the hat."));
  [%expect {| the|cat|the|hat |}]

let%expect_test "words drops the empty pieces" =
  print_endline (String.concat "|" (words "  two   spaces  "));
  [%expect {| two|spaces |}]

let%expect_test "a sentence" =
  print_endline (String.concat "|" (words "Emacs, the extensible editor!"));
  [%expect {| |}]
