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
-- Name: trips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trips (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    offer_id uuid NOT NULL,
    driver_id uuid,
    status character varying(50) DEFAULT 'pending_driver'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    pending_confirm_to character varying(50),
    driver_confirmed_at timestamp with time zone,
    dispatcher_confirmed_at timestamp with time zone,
    agreed_price numeric(18,2) DEFAULT 0 NOT NULL,
    agreed_currency character varying(3) DEFAULT 'UZS'::character varying NOT NULL,
    rating_from_driver numeric(3,1),
    rating_from_dispatcher numeric(3,1),
    rating_driver_to_dm integer,
    rating_dm_to_driver integer,
    rating_dm_to_cm integer,
    rating_cm_to_dm integer,
    CONSTRAINT trips_pending_confirm_check CHECK (((pending_confirm_to IS NULL) OR ((pending_confirm_to)::text = ANY ((ARRAY['IN_TRANSIT'::character varying, 'DELIVERED'::character varying, 'COMPLETED'::character varying])::text[])))),
    CONSTRAINT trips_rating_cm_to_dm_check CHECK (((rating_cm_to_dm >= 1) AND (rating_cm_to_dm <= 5))),
    CONSTRAINT trips_rating_dm_to_cm_check CHECK (((rating_dm_to_cm >= 1) AND (rating_dm_to_cm <= 5))),
    CONSTRAINT trips_rating_dm_to_driver_check CHECK (((rating_dm_to_driver >= 1) AND (rating_dm_to_driver <= 5))),
    CONSTRAINT trips_rating_driver_to_dm_check CHECK (((rating_driver_to_dm >= 1) AND (rating_driver_to_dm <= 5))),
    CONSTRAINT trips_status_check CHECK (((status)::text = ANY ((ARRAY['IN_PROGRESS'::character varying, 'IN_TRANSIT'::character varying, 'DELIVERED'::character varying, 'COMPLETED'::character varying, 'CANCELLED'::character varying])::text[])))
);


--
-- Name: trips trips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT trips_pkey PRIMARY KEY (id);


--
-- Name: idx_trips_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_cargo_id ON public.trips USING btree (cargo_id);


--
-- Name: idx_trips_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_driver_id ON public.trips USING btree (driver_id) WHERE (driver_id IS NOT NULL);


--
-- Name: idx_trips_offer_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_offer_id ON public.trips USING btree (offer_id);


--
-- Name: idx_trips_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trips_status ON public.trips USING btree (status);


--
-- Name: trips fk_trips_driver; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT fk_trips_driver FOREIGN KEY (driver_id) REFERENCES public.drivers(id);


--
-- Name: trips fk_trips_offer; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT fk_trips_offer FOREIGN KEY (offer_id) REFERENCES public.offers(id);


--
-- Name: trips trips_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trips
    ADD CONSTRAINT trips_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


