(* Word counts for a line of text. *)

(* The words of [line]: split at spaces, empty pieces dropped. *)
let words line = String.split_on_char ' ' line |> List.filter (fun w -> w <> "")

(* How many words [line] has. *)
let count_words _line = failwith "TODO: count the words of the line"

let%expect_test "count_words" =
  print_int (count_words "the cat  sat on the mat");
  [%expect {| 6 |}]
