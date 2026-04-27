--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: trip_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trip_ratings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    trip_id uuid NOT NULL,
    rater_kind character varying(16) NOT NULL,
    rater_id uuid NOT NULL,
    ratee_kind character varying(16) NOT NULL,
    ratee_id uuid NOT NULL,
    stars double precision NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT trip_ratings_ratee_kind_check CHECK (((ratee_kind)::text = ANY ((ARRAY['driver'::character varying, 'dispatcher'::character varying])::text[]))),
    CONSTRAINT trip_ratings_rater_kind_check CHECK (((rater_kind)::text = ANY ((ARRAY['driver'::character varying, 'dispatcher'::character varying, 'driver_manager'::character varying])::text[]))),
    CONSTRAINT trip_ratings_stars_range CHECK (((stars >= (1)::double precision) AND (stars <= (5)::double precision)))
);


--
-- Name: trip_ratings trip_ratings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_ratings
    ADD CONSTRAINT trip_ratings_pkey PRIMARY KEY (id);


--
-- Name: trip_ratings trip_ratings_trip_rater_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_ratings
    ADD CONSTRAINT trip_ratings_trip_rater_unique UNIQUE (trip_id, rater_kind, ratee_kind, ratee_id);


--
-- Name: idx_trip_ratings_ratee; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trip_ratings_ratee ON public.trip_ratings USING btree (ratee_kind, ratee_id);


--
-- Name: trip_ratings trip_ratings_trip_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_ratings
    ADD CONSTRAINT trip_ratings_trip_id_fkey FOREIGN KEY (trip_id) REFERENCES public.trips(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


