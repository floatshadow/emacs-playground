(* Invariants of Percentile.percentile, checked on every prefix of a
   fixed list of samples and on q from 0.0 to 1.0:
   1. the result is one of the samples;
   2. it grows (or stays) when q grows.
   dune test runs this; any exception or failed check stops it. *)

let samples = [ 12.; 15.; 11.; 19.; 14.; 95.; 16.; 12.; 240.; 13. ]
let qs = List.init 101 (fun i -> float_of_int i /. 100.)

let check xs =
  let ps = List.map (Percentile.percentile xs) qs in
  List.iter (fun p -> assert (List.mem p xs)) ps;
  ignore
    (List.fold_left
       (fun prev p ->
         assert (prev <= p);
         p)
       neg_infinity ps)

let () =
  List.iteri
    (fun i _ -> check (List.filteri (fun j _ -> j <= i) samples))
    samples;
  print_endline "percentile: all invariants hold"
