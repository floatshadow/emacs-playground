(* Prints a few statistics; dune test compares the output with
   test_stats.expected and shows the difference. *)

let xs = [ 3; 1; 4; 1; 5; 9; 2; 6 ]

let () =
  print_endline (Stats.show xs);
  Printf.printf "sum %d\n" (Stats.sum xs);
  (match Stats.mean xs with
  | Some m -> Printf.printf "mean %.3f\n" m
  | None -> print_endline "mean none");
  (match Stats.largest xs with
  | Some m -> Printf.printf "largest %d\n" m
  | None -> print_endline "largest none");
  Printf.printf "above 4: %d\n" (Stats.count_above 4 xs)
